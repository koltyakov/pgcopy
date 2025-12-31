package copier

import (
	"strconv"
	"strings"
	"testing"
)

// BenchmarkItoa benchmarks the pre-allocated itoa vs strconv.Itoa.
func BenchmarkItoa(b *testing.B) {
	b.Run("prealloc", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 1; j <= 1000; j++ {
				_ = itoa(j)
			}
		}
	})

	b.Run("strconv", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			for j := 1; j <= 1000; j++ {
				_ = strconv.Itoa(j)
			}
		}
	})
}

// BenchmarkBuildValuesClause benchmarks VALUES clause construction.
func BenchmarkBuildValuesClause(b *testing.B) {
	testCases := []struct {
		name    string
		numCols int
		numRows int
	}{
		{"10cols_100rows", 10, 100},
		{"20cols_500rows", 20, 500},
		{"50cols_100rows", 50, 100},
	}

	for _, tc := range testCases {
		b.Run(tc.name+"_optimized", func(b *testing.B) {
			b.ReportAllocs()
			sb := getStringBuilder()
			defer putStringBuilder(sb)
			for i := 0; i < b.N; i++ {
				sb.Reset()
				buildValuesClause(sb, tc.numCols, tc.numRows, 1)
				_ = sb.String()
			}
		})

		b.Run(tc.name+"_original", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var sb strings.Builder
				param := 1
				for row := 0; row < tc.numRows; row++ {
					if row > 0 {
						sb.WriteString(", ")
					}
					sb.WriteByte('(')
					for col := 0; col < tc.numCols; col++ {
						if col > 0 {
							sb.WriteString(", ")
						}
						sb.WriteByte('$')
						sb.WriteString(strconv.Itoa(param))
						param++
					}
					sb.WriteByte(')')
				}
				_ = sb.String()
			}
		})
	}
}

// BenchmarkStringBuilderPool benchmarks the string builder pool vs direct allocation.
func BenchmarkStringBuilderPool(b *testing.B) {
	b.Run("pooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sb := getStringBuilder()
			sb.WriteString("INSERT INTO test_table (col1, col2, col3) VALUES ")
			buildValuesClause(sb, 3, 100, 1)
			_ = sb.String()
			putStringBuilder(sb)
		}
	})

	b.Run("unpooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var sb strings.Builder
			sb.WriteString("INSERT INTO test_table (col1, col2, col3) VALUES ")
			param := 1
			for j := 0; j < 100; j++ {
				if j > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("(")
				for k := 0; k < 3; k++ {
					if k > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString("$")
					sb.WriteString(strconv.Itoa(param))
					param++
				}
				sb.WriteString(")")
			}
			_ = sb.String()
		}
	})
}

// BenchmarkRowBufferPool benchmarks the row buffer pool operations.
func BenchmarkRowBufferPool(b *testing.B) {
	b.Run("pooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf := getRowBuffer(20)
			// Simulate scanning
			for j := range buf {
				buf[j] = j
			}
			putRowBuffer(buf)
		}
	})

	b.Run("unpooled", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			buf := make([]any, 20)
			// Simulate scanning
			for j := range buf {
				buf[j] = j
			}
			_ = buf
		}
	})
}

// BenchmarkInsertStatementConstruction benchmarks the full INSERT statement construction.
func BenchmarkInsertStatementConstruction(b *testing.B) {
	numCols := 10
	numRows := 100
	columnList := "col1, col2, col3, col4, col5, col6, col7, col8, col9, col10"
	tableName := "\"public\".\"test_table\""

	// Simulate scanned data
	scanned := make([][]any, numRows)
	for i := range scanned {
		row := make([]any, numCols)
		for j := range row {
			row[j] = i*numCols + j
		}
		scanned[i] = row
	}

	b.Run("optimized", func(b *testing.B) {
		b.ReportAllocs()
		insertHeader := "INSERT INTO " + tableName + " (" + columnList + ") VALUES "
		for i := 0; i < b.N; i++ {
			sb := getStringBuilder()
			sb.WriteString(insertHeader)
			buildValuesClause(sb, numCols, len(scanned), 1)

			// Flatten args
			args := make([]any, 0, numRows*numCols)
			for _, row := range scanned {
				args = append(args, row...)
			}

			_ = sb.String()
			_ = args
			putStringBuilder(sb)
		}
	})

	b.Run("original", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var sb strings.Builder
			sb.WriteString("INSERT INTO ")
			sb.WriteString(tableName)
			sb.WriteString(" (")
			sb.WriteString(columnList)
			sb.WriteString(") VALUES ")

			args := make([]any, 0, numRows*numCols)
			param := 1

			for j, row := range scanned {
				if j > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString("(")
				for k := range numCols {
					if k > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString("$")
					sb.WriteString(strconv.Itoa(param))
					param++
					args = append(args, row[k])
				}
				sb.WriteString(")")
			}

			_ = sb.String()
			_ = args
		}
	})
}
