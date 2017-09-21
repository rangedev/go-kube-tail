package main

import (
	"fmt"
	"log"
	"flag"
	"os"
	"sync"
	"strings"
	"regexp"
	"time"
	"encoding/json"
	"os/signal"
	"syscall"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/net/context"
	"cloud.google.com/go/pubsub"
)

type Config struct {
	Project string `json:"projectName"`
	Topic string `json:"topicName"`
	Subscription string `json:"subscriptionName"`
	PodString string `json:"podString"`
	Namespace string `json:"namespaceName"`
}

type Msg struct {
	JsonPayload json.RawMessage `json:"jsonPayload"`
	Labels struct {
		PodName string `json:"container.googleapis.com/pod_name"`
		NamespaceName string `json:"container.googleapis.com/namespace_name"`
	} `json:"labels"`
	Resource struct {
		Labels struct {
			ProjectId string `json:"project_id"`
			Zone string `json:"zone"`
			ContainerName string `json:"container_name"`
		} `json:"labels"`
	} `json:"resource"`
	Timestamp string `json:"timestamp"`
	TextPayload string `json:"textPayload"`
}

type MsgString struct {
	MsgField string `json:"message"`
}

func main() {
	ctx := context.Background()
	homeDir := os.Getenv("HOME")
	defConf := homeDir + "/.go-kube-tail/config.json"

	var containerName string
	var namespace string
	var configFile string
	var podString string
	config := Config{}

	flag.Usage = func() {
		flag.PrintDefaults()
	}

	flag.StringVar(&namespace, "n", "", "Specify namespace name, defaults to empty")
	flag.StringVar(&containerName, "c", "", "Specify container name, defaults to empty")
	flag.StringVar(&configFile, "C", defConf, "Specify config file, defaults to conf.json")
	flag.StringVar(&podString, "p", "", "Specify pod regex (substring ok too)")

	flag.Parse()

	// Load Config
	file, _ := os.Open(configFile)
	decoder := json.NewDecoder(file)
	err := decoder.Decode(&config)
	if err != nil {
		log.Fatalf("Failed to load config file '%s': %v", configFile, err)
	}

	// Sets the name for the new topic.
	topicName := config.Topic
	subName := config.Subscription

        var podRegex = regexp.MustCompile(podString)

	log.Print("Setting up logger...")
	log.Printf("Topic: %s", topicName)
	log.Printf("Subscription: %s", subName)
	if containerName != "" {
		log.Printf("Container Name: %s", containerName)
	}
	if namespace != "" {
		log.Printf("Namespace: %s", namespace)
	}
	if podString != "" {
		log.Printf("Pod Name Regex: '%s'", podString)
	}

        // Creates a client.
        client, err := pubsub.NewClient(ctx, config.Project)
        if err != nil {
                log.Fatalf("Unable to create client: %v", err)
        }

        // Find topic
        topic := getTopic(client, topicName)

        // Create a new subscription.
        if err := create(client, subName, topic); err != nil {
                log.Fatalf("Unable to create subscription: %v", err)
        }

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Print(sig)
		log.Printf("Deleting subscription '%s'...", subName)
		if err := delete(client, subName); err != nil {
			log.Fatalf("Unable to delete subscription: %v", err)
		}
		os.Exit(0)
	}()

	// Pull messages via the subscription.
	if err := subscribe(client, subName, topic, containerName, namespace, podRegex); err != nil {
		log.Fatal(err)
	}
	if err := delete(client, subName); err != nil {
		log.Fatal(err)
	}
}

func stripCtlAndExtFromUnicode(str string) string {
	isOk := func(r rune) bool {
		return r < 32 || r >= 127
	}
	// The isOk filter is such that there is no need to chain to norm.NFC
	t := transform.Chain(norm.NFKD, transform.RemoveFunc(isOk))
	// This Transformer could also trivially be applied as an io.Reader
	// or io.Writer filter to automatically do such filtering when reading
	// or writing data anywhere.
	str, _, _ = transform.String(t, str)
	return str
}

func create(client *pubsub.Client, name string, topic *pubsub.Topic) error {
	ctx := context.Background()

	s := client.Subscription(name)
        ok, err := s.Exists(ctx)
        if err != nil {
                log.Fatal(err)
        }
        if ok {
                return nil
        }
	sub, err := client.CreateSubscription(ctx, name, pubsub.SubscriptionConfig{
		Topic:       topic,
		AckDeadline: 20 * time.Second,
	})
	if err != nil {
		return err
	}
	log.Printf("Created subscription: %v\n", sub)
	return nil
}

func handleMessage(msg *pubsub.Message, containerName string, namespace string, podRegex *regexp.Regexp) error {

	var summary string = "Unable to determine text log"
	parsed := &Msg{}
	err := json.Unmarshal(msg.Data, parsed)
	if err != nil {
		log.Printf("Error parsing:: %v\n", err)
	}

	// Exclude messages that don't match ContainerName if set
	if containerName != "" && parsed.Resource.Labels.ContainerName != containerName {
		msg.Ack()
		return nil
	}

	// Exclude messages that don't match namespace if set
	if namespace != "" && parsed.Labels.NamespaceName != namespace {
		msg.Ack()
		return nil
	}

	// Exclude messages that don't match the pod name regex
  	if podRegex != nil && podRegex.MatchString(parsed.Labels.PodName) == false {
		msg.Ack()
		return nil
	}

	// Get the summary string. If we find a TextPayload prefer that. 
	// Otherwise we attempt to find a field named "message" in a JsonPayload field. 
	// If we find neither just print the raw json
	if parsed.TextPayload != "" {
		summary = strings.TrimSpace(parsed.TextPayload)
	} else {
                var messageField MsgString
                err := json.Unmarshal(parsed.JsonPayload, &messageField)
                if err != nil { fmt.Println("error:", err) }

                if messageField.MsgField != "" {
			summary = messageField.MsgField
		} else {
			summary = string(parsed.JsonPayload)
		}
	}

	// TODO: create customizable log formats
	fmt.Printf("%s [%s]: %s\n",
		parsed.Timestamp,
		parsed.Labels.PodName,
		// parsed.Resource.Labels.ContainerName,
		summary )
	msg.Ack()
	return err
}

func subscribe(client *pubsub.Client, 
		name string, 
		topic *pubsub.Topic, 
		containerName string, 
		namespace string,
		podRegex *regexp.Regexp) error {
	var mu sync.Mutex
	received := 0
	ctx := context.Background()

	sub := client.Subscription(name)
	cctx, cancel := context.WithCancel(ctx)
	err := sub.Receive(cctx, func(ctx context.Context, msg *pubsub.Message) {
		mu.Lock()
		defer mu.Unlock()
		received++
		if received >= 1000000 {
			cancel()
			msg.Nack()
			return
		}

		if err := handleMessage(msg, containerName, namespace, podRegex); err != nil {
			log.Fatal(err)
		}
	})
	if err != nil {
		return err
	}
	return nil
}

func getTopic(c *pubsub.Client, name string) *pubsub.Topic {
	ctx := context.Background()

	// Create a topic to subscribe to.
	t := c.Topic(name)
	ok, err := t.Exists(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if ok {
		return t
	} else {
		log.Fatalf("Topic '%s' doesn't exist", name)
	}
	return nil
}

func delete(client *pubsub.Client, name string) error {
	ctx := context.Background()
	sub := client.Subscription(name)
	if err := sub.Delete(ctx); err != nil {
		return err
	}
	log.Printf("Subscription '%s' deleted.", name)
	return nil
}
