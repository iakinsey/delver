package fsm

type FSMStates interface {
	Next() error
	HasNext() bool
	GetResult() []string
}

type FSM interface {
	Perform() ([]string, error)
}

type fsm struct {
	FSMStates
}

func NewFSM(states FSMStates) FSM {
	return &fsm{states}
}

func (f *fsm) Perform() ([]string, error) {
	for f.HasNext() {
		if err := f.Next(); err != nil {
			return f.GetResult(), err
		}
	}

	return f.GetResult(), nil
}
