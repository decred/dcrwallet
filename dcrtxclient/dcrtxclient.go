package dcrtxclient

import (
	"fmt"
	"sync"

	"github.com/decred/dcrwallet/dcrtxclient/service"
	"google.golang.org/grpc"
)

type (
	Config struct {
		Enable  bool
		Address string
		Timeout uint32
	}

	Client struct {
		sync.Mutex
		cfg  *Config
		conn *grpc.ClientConn
		*service.TransactionService
	}
)

func SetConfig(cfg *Config) *Client {
	client := &Client{
		cfg: cfg,
	}
	return client
}

func (c *Client) StartSession() (*Client, error) {
	if c.cfg.Enable {
		// connect to dcrtxmatcher server if enable
		conn, err := c.Connect()
		if err != nil {
			return nil, err
		}

		// somehow conn object is still nil
		if conn == nil {
			return nil, ErrCannotConnect
		}

		c.conn = conn

		// register services
		c.registerServices()
	}

	return c, nil
}

func NewClient(cfg *Config) (*Client, error) {
	client := &Client{
		cfg: cfg,
	}

	if cfg.Enable {
		// connect to dcrtxmatcher server if enable
		conn, err := client.Connect()
		if err != nil {
			return nil, err
		}

		// somehow conn object is still nil
		if conn == nil {
			return nil, ErrCannotConnect
		}

		client.conn = conn

		// register services
		client.registerServices()
	}

	return client, nil
}

func (c *Client) Config() *Config {
	return c.cfg
}

// connect attempts to connect to our dcrtxmatcher server
func (c *Client) Connect() (*grpc.ClientConn, error) {
	c.Lock()
	defer c.Unlock()

	//	if c.isConnected() {
	//		return nil, ErrAlreadyConnected
	//	}

	conn, err := grpc.Dial(c.cfg.Address, grpc.WithInsecure())
	if err != nil {
		log.Warn("Unable to connect to dcrtxmatcher server")
		return nil, err
	}

	log.Info("Successfull connection with dcrtxmatcher server")
	return conn, nil

}

// Disconnect disconnects client from server
// returns error if client is not connected
func (c *Client) Disconnect() error {
	if c.isConnected() {
		c.conn.Close()
		log.Info("dcrTxClient disconnected")
		return nil
	}
	return ErrNotConnected
}

// isConnected checks if client is connected to server
// returns appropriate boolen depending on result
func (c *Client) isConnected() bool {
	if c.conn != nil {
		return true
	}

	return false
}

func (c *Client) registerServices() error {
	if !c.isConnected() {
		return ErrNotConnected
	}

	c.TransactionService = service.NewTransactionService(c.conn)

	return nil
}
