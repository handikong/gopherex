package engine

import "encoding/json"

type cmdJSON struct {
	V   uint8   `json:"v"`
	Seq uint64  `json:"seq"`
	Cmd Command `json:"cmd"`
}

type JSONCmdCodec struct{ Version uint8 }

func (c JSONCmdCodec) Encode(dst []byte, seq uint64, cmd Command) ([]byte, error) {
	rec := cmdJSON{V: c.Version, Seq: seq, Cmd: cmd}
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	return append(dst, b...), nil
}
func (c JSONCmdCodec) Decode(payload []byte) (uint64, Command, error) {
	var rec cmdJSON
	if err := json.Unmarshal(payload, &rec); err != nil {
		return 0, Command{}, err
	}
	return rec.Seq, rec.Cmd, nil
}

type evJSON struct {
	V  uint8 `json:"v"`
	Ev Event `json:"ev"`
}

type JSONEvCodec struct{ Version uint8 }

func (c JSONEvCodec) Encode(dst []byte, ev Event) ([]byte, error) {
	rec := evJSON{V: c.Version, Ev: ev}
	b, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}
	return append(dst, b...), nil
}
func (c JSONEvCodec) Decode(payload []byte) (Event, error) {
	var rec evJSON
	if err := json.Unmarshal(payload, &rec); err != nil {
		return Event{}, err
	}
	return rec.Ev, nil
}
