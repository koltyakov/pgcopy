package copier

import (
	"context"
	"errors"
	"fmt"

	"github.com/koltyakov/pgcopy/internal/state"
)

// Layer interfaces introduce separation of concerns. Initial implementation
// is thin wrappers around existing Copier methods so functionality remains
// unchanged while enabling future replacement.

// Discovery is responsible for table / FK discovery and basic stats.
type Discovery interface {
	DiscoverTables() ([]*TableInfo, error)
	DetectForeignKeys(tables []*TableInfo) error
}

// Planner orders tables & resolves dependencies (currently passthrough).
type Planner interface {
	PlanTables(tables []*TableInfo) ([]*TableInfo, error)
	PlanLayers(tables []*TableInfo) ([][]*TableInfo, error)
}

// Executor runs the data movement for a planned set.
type Executor interface {
	Execute(ctx context.Context, tables []*TableInfo) error
}

// Reporter handles state broadcasting / console rendering (future use).
type Reporter interface {
	// Reserved for future methods (e.g., Tick/Finalize). Placeholder.
}

// Persistence handles durable logging / summaries.
type Persistence interface {
	// Placeholder for future save/flush methods.
}

// ProgressSink receives progress events decoupled from execution.
type ProgressSink interface {
	UpdateTable(schema, table string, rowsCopied int64)
	Log(level, msg, scope, table string)
	Done()
}

// ForeignKeyStrategy abstracts FK handling modes.
type ForeignKeyStrategy interface {
	Detect(tables []*TableInfo) error
	Cleanup() error // after all tables
}

// defaultDiscovery bridges to existing copier methods.
type defaultDiscovery struct{ c *Copier }

func (d *defaultDiscovery) DiscoverTables() ([]*TableInfo, error) { return d.c.getTablesToCopy() }
func (d *defaultDiscovery) DetectForeignKeys(tables []*TableInfo) error {
	return d.c.fkManager.DetectForeignKeys(tables)
}

// defaultPlanner currently returns input as-is.
type defaultPlanner struct{ c *Copier }

func (p *defaultPlanner) PlanTables(tables []*TableInfo) ([]*TableInfo, error) { return tables, nil }
func (p *defaultPlanner) PlanLayers(tables []*TableInfo) ([][]*TableInfo, error) {
	return p.c.buildDependencyLayers(tables), nil
}

// defaultExecutor invokes existing parallel copy logic.
type defaultExecutor struct{ c *Copier }

func (e *defaultExecutor) Execute(ctx context.Context, tables []*TableInfo) error {
	layers, err := e.c.planner.PlanLayers(tables)
	if err != nil || len(layers) == 0 {
		return e.c.copyTablesParallel(ctx, tables)
	}

	e.c.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Dependency graph constructed: %d layer(s); processing each layer sequentially.", len(layers)), "copier", "", nil)
	var layerErrors []error
	for i, layer := range layers {
		if len(layer) == 0 {
			continue
		}
		e.c.state.AddLog(state.LogLevelInfo, fmt.Sprintf("Starting dependency layer %d/%d with %d table(s)", i+1, len(layers), len(layer)), "copier", "", nil)
		if err := e.c.copyTablesParallel(ctx, layer); err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			layerErrors = append(layerErrors, err)
		}
	}
	return errors.Join(layerErrors...)
}

// defaultReporter is a no-op placeholder.
type defaultReporter struct{ c *Copier }

// defaultPersistence is a no-op placeholder (file logger already initialized).
type defaultPersistence struct{ c *Copier }
