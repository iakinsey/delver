package fsm

import "os"

type FSMStates interface {
	Init(*os.File)
	Next() error
	HasNext() bool
	GetResult() []string
}

type FSM interface {
	Perform(*os.File) ([]string, error)
}

type fsm struct {
	FSMStates
}

func NewFSM(states FSMStates) FSM {
	return &fsm{states}
}

func (s *fsm) Perform(f *os.File) ([]string, error) {
	s.Init(f)

	for s.HasNext() {
		if err := s.Next(); err != nil {
			return s.GetResult(), err
		}
	}

	return s.GetResult(), nil
}
