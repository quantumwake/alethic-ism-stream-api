package proxy

//type NATSProxyStream struct {
//	NATSProxy
//}
//
//func NewNATSProxyStream(url string) *NATSProxyStream {
//	stream := &NATSProxyStream{
//		NATSProxy: NATSProxy{
//			NATSUrl: url,
//		},
//	}
//
//	//// Assign the instance method as the PoolCreator function
//	//stream.PoolCreator = func(subject string) (*pool.PoolInterface, error) {
//	//	return stream.NewPool(subject)
//	//}
//
//	return stream
//}
//
//func (p *NATSProxyStream) NewPool(subject string) (*pool.StreamPool, error) {
//	streamPool, err := pool.NewStreamSessionPool(p.NATSUrl, subject)
//	if err != nil {
//		return nil, err
//	}
//	return streamPool, nil
//}
