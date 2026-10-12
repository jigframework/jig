// Package jobs is a transactional background job queue backed by Postgres.
// A job enqueued inside a transaction becomes visible only if that
// transaction commits, so a record and the work it triggers are never out of
// sync. Workers claim jobs with SELECT ... FOR UPDATE SKIP LOCKED.
package jobs
