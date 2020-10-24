package job

// TODO improve comments

// Job allows you to easily run tasks as standalone goroutines
// as well as via `workerpool`
type Job struct {
	Run    func()
	data   interface{}
	err    error
	name   string
	result chan Result
}

// NewJob creates a new job
func NewJob(name string, resultChan chan Result, todo func() (interface{}, error)) Job {
	j := Job{
		name:   name,
		result: resultChan,
	}

	j.Run = func() {
		res, err := todo()
		j.result <- &result{
			name: j.name,
			data: res,
			err:  err,
		}
	}

	return j
}

// Name returns the job name
func (j *Job) Name() string {
	return j.name
}
