package pool

type StreamPool struct {
	*Pool
}

func NewStreamSessionPool(natsURL string, subject string) (*StreamPool, error) {
	basePool, err := NewSessionPool(natsURL, subject)
	return &StreamPool{
		Pool: basePool,
	}, err
}
