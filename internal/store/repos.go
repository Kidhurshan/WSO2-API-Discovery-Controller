package store

import "github.com/wso2/adc/internal/logging"

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
func NewRepositories(db *DB, logger *logging.Logger) *Repositories {
	return &Repositories{
		Discovered:    NewDiscoveredRepo(db, logger),
		Managed:       NewManagedRepo(db, logger),
		ManagedOps:    NewManagedOpsRepo(db, logger),
		Unmanaged:     NewUnmanagedRepo(db, logger),
		PipelineState: NewPipelineStateRepo(db, logger),
		db:            db,
	}
}

// DB returns the underlying database connection.
func (r *Repositories) DB() *DB {
	return r.db
}
