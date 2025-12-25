package ws

func BridgeRaw(hub *Hub, topic string, payload []byte) {
	hub.Publish(topic, payload) // 你已有 publish：更新 last + fanout
}
