package copier

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
}

// Executor runs the data movement for a planned set.
type Executor interface {
	Execute(tables []*TableInfo) error
}

// Reporter handles state broadcasting / console rendering (future use).
type Reporter interface {
	// Reserved for future methods (e.g., Tick/Finalize). Placeholder.
}

// Persistence handles durable logging / summaries.
type Persistence interface {
	// Placeholder for future save/flush methods.
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

// defaultExecutor invokes existing parallel copy logic.
type defaultExecutor struct{ c *Copier }

func (e *defaultExecutor) Execute(tables []*TableInfo) error { return e.c.copyTablesParallel(tables) }

// defaultReporter is a no-op placeholder.
type defaultReporter struct{ c *Copier }

// defaultPersistence is a no-op placeholder (file logger already initialized).
type defaultPersistence struct{ c *Copier }
