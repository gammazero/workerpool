package job

// Result holds job result
type Result interface {
	JobName() string
	Err() error
	Data() interface{}
}

// result implements Result
type result struct {
	name string
	data interface{}
	err  error
}

func (r *result) JobName() string {
	return r.name
}

func (r *result) Err() error {
	return r.err
}

func (r *result) Data() interface{} {
	return r.data
}
