package messages

//Message parser for marchal and unmarshal from/to protobuf message
//MessageType is id of message client/server send in each round
//Data is data of the round that is protobuf messsage
import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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
)

type (
	Message struct {
		MsgType uint32
		Data    []byte
	}
)

func NewMessage(msgtype uint32, data []byte) *Message {
	return &Message{
		MsgType: msgtype,
		Data:    data,
	}
}

func BytesToUint(data []byte) (ret uint32) {
	buf := bytes.NewBuffer(data)
	binary.Read(buf, binary.BigEndian, &ret)
	return
}

//Parse data received (from client or server) to Message
func ParseMessage(msgData []byte) (*Message, error) {
	if len(msgData) < 4 {
		return nil, errors.New("message data is less than 4 bytes")
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

func (msg *Message) ToBytes() []byte {
	msgData := IntToBytes(msg.MsgType)
	msgData = append(msgData, msg.Data...)

	return msgData
}
func IntToBytes(val uint32) []byte {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, uint32(val))
	if err != nil {
		fmt.Println("binary.write error", err)
	}
	return buf.Bytes()
}
