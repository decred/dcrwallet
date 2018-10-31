package messages

//Message parser for marchal and unmarshal from/to protobuf message
//MessageType is id of message client/server send in each round
//Data is data of the round that is protobuf messsage
import (
	"bytes"
	"encoding/binary"
	"errors"
)

const (
	C_JOIN_REQUEST      = 1
	C_KEY_EXCHANGE      = 2
	C_DC_EXP_VECTOR     = 3
	C_DC_SIMPLE_VECTOR  = 4
	C_DC_XOR_VECTOR     = 5
	C_TX_INPUTS         = 6
	C_TX_SIGN           = 7
	C_TX_PUBLISH_RESULT = 8
)

const (
	S_JOIN_RESPONSE     = 100
	S_KEY_EXCHANGE      = 101
	S_HASHES_VECTOR     = 102
	S_MIXED_TX          = 103
	S_JOINED_TX         = 104
	S_TX_SIGN           = 105
	S_DC_EXP_VECTOR     = 106
	S_DC_XOR_VECTOR     = 107
	S_TX_PUBLISH_RESULT = 108
	S_MALICIOUS_PEERS   = 109
)

const (
	// Size of dc-net exponential vector element.
	ExpRandSize = 12
	// Size of dc-net xor vector element, the same with lengh of pkscript.
	PkScriptSize     = 25
	PkScriptHashSize = 16
)

type (
	Message struct {
		MsgType uint32
		Data    []byte
	}
)

// NewMessage constructs the message from message type and bytes data.
func NewMessage(msgtype uint32, data []byte) *Message {
	return &Message{
		MsgType: msgtype,
		Data:    data,
	}
}

// BytesToUint converts slice of bytes to uint32.
func BytesToUint(data []byte) (ret uint32) {
	buf := bytes.NewBuffer(data)
	binary.Read(buf, binary.BigEndian, &ret)
	return
}

// ParseMessage unmarshals data received (from client or server) to Message.
func ParseMessage(msgData []byte) (*Message, error) {
	if len(msgData) < 4 {
		return nil, errors.New("Message data is less than 4 bytes")
	}

	cmd := msgData[:4]

	msgType := BytesToUint(cmd)
	data := []byte{}
	if len(msgData) > 4 {
		data = msgData[4:]
	}

	return &Message{
		MsgType: msgType,
		Data:    data,
	}, nil

}

// ToBytes marshals message data to bytes.
func (msg *Message) ToBytes() []byte {
	msgData, _ := IntToBytes(msg.MsgType)
	msgData = append(msgData, msg.Data...)

	return msgData
}

// IntToBytes converts uint32 to bytes.
func IntToBytes(val uint32) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, uint32(val))
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
