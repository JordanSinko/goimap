package goimap

type Settings struct {
	logger Logger
}

func NewSettings() *Settings {
	return &Settings{
		logger: NewNoopLogger(),
	}
}
