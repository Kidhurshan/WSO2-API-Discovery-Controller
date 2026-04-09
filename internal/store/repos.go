package store

// Repositories aggregates all repository instances.
type Repositories struct {
	Discovered    *DiscoveredRepo
	Managed       *ManagedRepo
	ManagedOps    *ManagedOpsRepo
	Unmanaged     *UnmanagedRepo
	PipelineState *PipelineStateRepo
	db            *DB
}

// NewRepositories creates all repositories from a shared DB connection.
func NewRepositories(db *DB) *Repositories {
	return &Repositories{
		Discovered:    NewDiscoveredRepo(db),
		Managed:       NewManagedRepo(db),
		ManagedOps:    NewManagedOpsRepo(db),
		Unmanaged:     NewUnmanagedRepo(db),
		PipelineState: NewPipelineStateRepo(db),
		db:            db,
	}
}

// DB returns the underlying database connection.
func (r *Repositories) DB() *DB {
	return r.db
}
