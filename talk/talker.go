package talk

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/makeitplay/arena"
	"github.com/sirupsen/logrus"
	"net"
	"net/http"
	"net/url"
	"sync"
)

type Talker interface {
	Connect(url url.URL, playerSpec arena.PlayerSpecifications) (ctx context.Context, err error)
	Send(data []byte) error
	Close()
}

// channel is meant to make the websocket connection and communication easier.
type channel struct {
	ws               *websocket.Conn
	playerSpec       arena.PlayerSpecifications
	urlConnection    url.URL
	onMessage        func(bytes []byte)
	onCloseByPeer    func()
	connectionCtx    context.Context
	connectionCloser context.CancelFunc
	//readingMitx      sync.Mutex
	writingMitx       sync.Mutex
	logger            *logrus.Entry
	connectionOpenned bool
}

func NewTalker(logger *logrus.Entry, onMessage func(bytes []byte), onCloseByPeer func()) Talker {
	return &channel{
		onMessage:     onMessage,
		onCloseByPeer: onCloseByPeer,
		logger:        logger,
	}
}

func (c *channel) Connect(url url.URL, playerSpec arena.PlayerSpecifications) (ctx context.Context, err error) {
	c.playerSpec = playerSpec
	c.urlConnection = url
	if err := c.dial(); err != nil {
		return nil, err
	}
	c.connectionCtx, c.connectionCloser = context.WithCancel(context.Background())
	c.connectionOpenned = true
	go c.keepListenning()

	return c.connectionCtx, nil
}

// Send allow the player to send a ws message to the game server
func (c *channel) Send(data []byte) error {
	c.writingMitx.Lock()
	defer c.writingMitx.Unlock()
	return c.ws.WriteMessage(websocket.TextMessage, data)
}

func (c *channel) Close() {
	c.connectionOpenned = false
	c.ws.WriteMessage(websocket.CloseNormalClosure, []byte("bye"))
	c.ws.Close()
}

func (c *channel) dial() error {
	connectHeader := http.Header{}
	specJson, err := json.Marshal(c.playerSpec)
	if err != nil {
		return fmt.Errorf("fail on bulding the player spec header: %s", err.Error())
	}
	connectHeader.Add("X-Player-Specs", string(specJson))

	c.ws, _, err = websocket.DefaultDialer.Dial(c.urlConnection.String(), connectHeader)
	if err != nil {
		return fmt.Errorf("fail on dialing to ws server: %s", err.Error())
	}
	return nil
}

func (c *channel) keepListenning() {
	for {
		msgType, message, err := c.ws.ReadMessage()
		if e, ok := err.(*websocket.CloseError); ok {
			if e.Code == websocket.CloseGoingAway || e.Code == websocket.CloseAbnormalClosure {
				c.onCloseByPeer()
			} else if e.Code == websocket.CloseNormalClosure && c.connectionOpenned {
				c.onCloseByPeer()
			} else {
				c.logger.Infof("Connection closed by the player (%d): %s", msgType, e)
			}
			if c.connectionOpenned { //something close not asked by us
				c.connectionCloser() //unexpected
			}
			return
		} else if e, ok := err.(net.Error); ok && c.connectionOpenned {
			c.connectionCloser() //unexpected
			c.logger.Infof("unnexpected connection closed (%d): %s", msgType, e)
			return
		} else if err != nil {
			c.connectionCloser() //unexpected
			return
		} else {
			c.onMessage(message)
		}
	}
}
