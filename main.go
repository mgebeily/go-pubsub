package main

import (
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/joho/godotenv"
	pasetoware "github.com/mgebeily/fiber-paseto"
)

type Config struct {
	ChannelLookup        string
	ErrorHandler         string
	PublishTokenLookup   string
	SubscribeTokenLookup string

	GetChannelSubscribers func(string) []chan string
	CanJoinChannel        func(*fiber.Ctx, string) bool
	PublishMiddleware     func(*fiber.Ctx) error
	SubscribeMiddleware   func(*fiber.Ctx) error

	AddToChannel      func(string) error
	RemoveFromChannel func(string) error
}

func main() {
	godotenv.Load()

	// app := &cli.App{
	// 	Name: "ws-pubsub",
	// 	Flags: []cli.Flag{
	// 		&cli.IntFlag{
	// 			Name:  "port",
	// 			Usage: "The port to start the server on.",
	// 			Value: 3000,
	// 		},
	// 		&cli.StringFlag{
	// 			Name:  "path",
	// 			Usage: "The path to start the server on.",
	// 			Value: "ws",
	// 		},
	// 		&cli.StringFlag{
	// 			Name:  "subscribe-auth",
	// 			Usage: "The type and location of the auth for subscribing to a channel.",
	// 			Value: "paseto:header:token",
	// 		},
	// 		&cli.StringFlag{
	// 			Name:  "publish-auth",
	// 			Usage: "The type and location of the auth for publishing to a channel.",
	// 			Value: "paseto:header:token",
	// 		},
	// 		&cli.StringFlag{
	// 			Name:  "channel-lookup",
	// 			Usage: "The location of the channel to operate on in the request.",
	// 			Value: "header.token",
	// 		},
	// 	},
	// 	Usage: "Start a pubsub server with the given options",
	// 	Action: func(c *cli.Context) error {
	// 		app := New(&Config{})
	// 		return app.Listen(":" + c.String("port"))
	// 	},
	// }

	// err := app.Run(os.Args)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	app, _ := New(&Config{})
	app.Listen(":3001")
}

// TODO: This with pure channels
type Connection struct {
	ChannelId  string
	Subscriber chan string
}

// TODO: Just call this NEW and mount it in the app?
func New(config *Config) (*fiber.App, func(string, string) error) {
	app := fiber.New()

	publisher := make(chan *Connection)
	channels := make(map[string][]chan string)

	if config.AddToChannel == nil {
		config.AddToChannel = func(channelId string) error {
			_, ok := channels[channelId]
			if !ok {
				channels[channelId] = []chan string{}
			}

			channels[channelId] = append(channels[channelId], make(chan string))

			return nil
		}
	}
	if config.GetChannelSubscribers == nil {
		config.GetChannelSubscribers = func(channelId string) []chan string {
			channel, ok := channels[channelId]
			if !ok {
				return []chan string{}
			} else {
				return channel
			}
		}
	}
	if config.SubscribeMiddleware == nil {
		config.SubscribeMiddleware = func(c *fiber.Ctx) error {
			return c.Next()
		}
	}
	if config.PublishMiddleware == nil {
		config.PublishMiddleware = func(c *fiber.Ctx) error {
			return c.Next()
		}
	}

	publish := func(channelId string, value string) error {
		channels := config.GetChannelSubscribers(channelId)

		for channel := range channels {
			channels[channel] <- value
		}

		return nil
	}

	// TODO: After middleware?
	app.Post("/publish/:channelId", config.PublishMiddleware, publishToChannel(config, publish))

	app.Use(func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			c.Locals("allowed", true)
			return c.Next()
		}

		return fiber.ErrUpgradeRequired
	})
	app.Get("/subscribe/:channelId",
		config.SubscribeMiddleware,
		func(c *fiber.Ctx) error {
			channel := make(chan string)
			publisher <- &Connection{ChannelId: c.Params("id"), Subscriber: channel}
			c.Locals("channel", channel)
			return c.Next()
		},
		subscribeToChannel(config))

	go func() {
		for channel := range publisher {
			channels[channel.ChannelId] = append(channels[channel.ChannelId], channel.Subscriber)
		}
	}()

	return app, publish
}

func publishToChannel(config *Config, publish func(string, string) error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := publish(c.Params("id"), string(c.Body()))

		if err != nil {
			return c.SendStatus(500)
		}

		return c.SendStatus(200)
	}
}

func subscribeToChannel(config *Config) fiber.Handler {
	return websocket.New(func(conn *websocket.Conn) {
		// TODO: Pass channel directly
		subscriber := conn.Locals("channel").(chan string)

		for value := range subscriber {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(value)); err != nil {
				log.Println("write:", err)
				break
			}
		}
	})
}

func createAuthMiddleware(tokenLookup string) func(c *fiber.Ctx) error {
	// Split the strings into parts representing type and token lookup
	parts := strings.Split(tokenLookup, ":")

	switch parts[0] {
	case "paseto":
		return pasetoware.New(&pasetoware.Config{
			TokenLookup: parts[1] + ":" + parts[2],
		})
	default:
		return func(c *fiber.Ctx) error {
			return c.Next()
		}
	}
}
