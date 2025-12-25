package md

type Event interface {
	IsEvent()
}

type Adapter interface {
	Name() string
	URL() string
	SubscribeMessages(products []string) [][]byte
	Decode(raw []byte, emit func(Event)) error
}
