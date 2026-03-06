package sagabus

import "context"

// DecodeWrap wraps a typed handler to work with MessageDecoder
// This is a helper function for integrating with other pubsub systems
func DecodeWrap[Params any, Result any](f Handler[Params, Result]) func(rawMsg *MessageDecoder) (out any, err error) {
	return func(rawMsg *MessageDecoder) (out any, err error) {
		var params Params
		if err := rawMsg.Decode(&params); err != nil {
			return nil, err
		}
		return f(context.Background(), params)
	}
}
