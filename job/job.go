package job

// Job knows how to run on it's own goroutine, as well as how to be submitted
// to a `*workerpool.WorkerPool`
type Job interface {
	Name() string
	Run()
}

// NewJob creates a new job
func NewJob(name string, resultChan chan Result, todo func() (interface{}, error)) Job {
	return &job{name: name, result: resultChan, todo: todo}
}

// job implements Job
type job struct {
	name   string
	result chan Result
	todo   func() (interface{}, error)
}

// Name returns job name
func (j *job) Name() string {
	return j.name
}

// Run invokes the job
func (j *job) Run() {
	res, err := j.todo()
	j.result <- Result{jobName: j.name, data: res, err: err}
}

// Result holds job result
type Result struct {
	jobName string
	data    interface{}
	err     error
}

// JobName returns the Job name
func (r *Result) JobName() string {
	return r.jobName
}

// Err returns Job error
func (r *Result) Err() error {
	return r.err
}

// Data returns Job data
func (r *Result) Data() interface{} {
	return r.data
}
