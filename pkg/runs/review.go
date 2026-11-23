package runs

// StartedReview reports whether the run turned from a reply into a review
// (Index.StartReview).
func (r Run) StartedReview() bool { return r.reviewStarted }

// StartReview turns a run that answers with a reply into a review, once.
// started is true for the one call that made the change, and false for
// every call after it, from this process or another: the change is written
// against the version of the record that was read, so of two calls made at
// once exactly one starts the review. It is kept on the run's record and
// outlives the run's events. An error means nothing was changed.
func (x *Index) StartReview(tenant, runID string) (started bool, err error) {
	err = x.do(tenant, func(t *txn) error {
		started = false
		r, err := t.run(runID)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNotFound
		}
		if r.reviewStarted {
			return errSkip
		}
		t.touch(r)
		r.reviewStarted = true
		started = true
		return nil
	})
	if err != nil {
		started = false
	}
	return started, err
}
