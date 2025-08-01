package cmd

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	_ "github.com/lib/pq"
	"github.com/spf13/cobra"
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List tables in a database",
	Long: `List all tables in the specified PostgreSQL database along with their row counts.
This helps you understand what will be copied before running the copy command.

Examples:
  pgcopy list --source "postgres://user:pass@localhost:5432/mydb"
  pgcopy list -s "postgres://user:pass@localhost:5432/mydb" --schema public`,
	Run: func(cmd *cobra.Command, args []string) {
		sourceConn, _ := cmd.Flags().GetString("source")
		sourceFile, _ := cmd.Flags().GetString("source-file")
		schema, _ := cmd.Flags().GetString("schema")

		if sourceConn == "" && sourceFile == "" {
			log.Fatal("Either --source connection string or --source-file must be provided")
		}

		var connStr string
		var err error

		if sourceFile != "" {
			content, err := os.ReadFile(sourceFile)
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
		defer db.Close()

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
		fmt.Fprintln(w, "SCHEMA\tTABLE\tROW COUNT\tSIZE")
		fmt.Fprintln(w, "------\t-----\t---------\t----")

		var totalRows int64
		for _, table := range tables {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
				table.Schema, table.Name, table.RowCount, table.Size)
			totalRows += table.RowCount
		}

		fmt.Fprintln(w, "------\t-----\t---------\t----")
		fmt.Fprintf(w, "TOTAL\t%d tables\t%d rows\t\n", len(tables), totalRows)
		w.Flush()
	},
}

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
	defer rows.Close()

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
