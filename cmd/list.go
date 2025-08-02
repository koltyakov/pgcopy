// Package cmd provides command-line interface for pgcopy
package cmd

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/spf13/cobra"
)

// formatNumber formats large numbers with K/M suffixes (same as in copy.go)
func formatNumberList(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dK", n/1000)
		}
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	if n < 1000000000 {
		if n%1000000 == 0 {
			return fmt.Sprintf("%dM", n/1000000)
		}
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n%1000000000 == 0 {
		return fmt.Sprintf("%dB", n/1000000000)
	}
	return fmt.Sprintf("%.1fB", float64(n)/1000000000)
}

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tables in a database",
	Long: `List all tables in the specified PostgreSQL database along with their row counts.
This helps you understand what will be copied before running the copy command.

Examples:
  pgcopy list --source "postgres://user:pass@localhost:5432/mydb"
  pgcopy list -s "postgres://user:pass@localhost:5432/mydb" --schema public`,
	Run: func(cmd *cobra.Command, _ []string) {
		sourceConn, _ := cmd.Flags().GetString("source")
		sourceFile, _ := cmd.Flags().GetString("source-file")
		schema, _ := cmd.Flags().GetString("schema")

		if sourceConn == "" && sourceFile == "" {
			log.Fatal("Either --source connection string or --source-file must be provided")
		}

		var connStr string
		var err error

		if sourceFile != "" {
			content, err := os.ReadFile(sourceFile) // #nosec G304 - file path from command line argument
			if err != nil {
				log.Fatalf("Failed to read source file: %v", err)
			}
			connStr = string(content)
		} else {
			connStr = sourceConn
		}

		db, err := sql.Open("postgres", connStr)
		if err != nil {
			log.Fatalf("Failed to connect to database: %v", err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				log.Printf("Failed to close database connection: %v", err)
			}
		}()

		if err = db.Ping(); err != nil {
			log.Fatalf("Failed to ping database: %v", err)
		}

		tables, err := getTables(db, schema)
		if err != nil {
			log.Fatalf("Failed to get tables: %v", err)
		}

		if len(tables) == 0 {
			fmt.Println("No tables found")
			return
		}

		// Print tables in a nice format
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "SCHEMA\tTABLE\tROW COUNT\tSIZE")
		_, _ = fmt.Fprintln(w, "------\t-----\t---------\t----")

		var totalRows int64
		for _, table := range tables {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				table.Schema, table.Name, formatNumberList(table.RowCount), table.Size)
			totalRows += table.RowCount
		}

		_, _ = fmt.Fprintln(w, "------\t-----\t---------\t----")
		_, _ = fmt.Fprintf(w, "TOTAL\t%d tables\t%s rows\t\n", len(tables), formatNumberList(totalRows))
		_ = w.Flush()
	},
}

// TableSummary represents information about a database table including schema, name, row count and size
type TableSummary struct {
	Schema   string
	Name     string
	RowCount int64
	Size     string
}

func getTables(db *sql.DB, schema string) ([]TableSummary, error) {
	var query string
	var args []interface{}

	if schema != "" {
		query = `
			SELECT 
				t.schemaname,
				t.tablename,
				COALESCE(s.n_tup_ins + s.n_tup_upd + s.n_tup_del, 0) as row_count,
				COALESCE(pg_size_pretty(pg_total_relation_size(c.oid)), 'N/A') as size
			FROM pg_tables t
			LEFT JOIN pg_stat_user_tables s ON t.schemaname = s.schemaname AND t.tablename = s.relname
			LEFT JOIN pg_class c ON c.relname = t.tablename AND c.relnamespace = (
				SELECT oid FROM pg_namespace WHERE nspname = t.schemaname
			)
			WHERE t.schemaname = $1
			ORDER BY t.schemaname, t.tablename`
		args = append(args, schema)
	} else {
		query = `
			SELECT 
				t.schemaname,
				t.tablename,
				COALESCE(s.n_tup_ins + s.n_tup_upd + s.n_tup_del, 0) as row_count,
				COALESCE(pg_size_pretty(pg_total_relation_size(c.oid)), 'N/A') as size
			FROM pg_tables t
			LEFT JOIN pg_stat_user_tables s ON t.schemaname = s.schemaname AND t.tablename = s.relname
			LEFT JOIN pg_class c ON c.relname = t.tablename AND c.relnamespace = (
				SELECT oid FROM pg_namespace WHERE nspname = t.schemaname
			)
			WHERE t.schemaname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
			ORDER BY t.schemaname, t.tablename`
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Failed to close rows: %v", err)
		}
	}()

	var tables []TableSummary
	for rows.Next() {
		var table TableSummary
		if err := rows.Scan(&table.Schema, &table.Name, &table.RowCount, &table.Size); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	return tables, rows.Err()
}

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().StringP("source", "s", "", "Source database connection string")
	listCmd.Flags().String("source-file", "", "Source database connection config file")
	listCmd.Flags().String("schema", "", "Specific schema to list (optional)")
}
