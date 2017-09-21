# go-kube-tail

Mostly me learning go for something I would actually use. i
My goal is to make use of a google cloud logging export (pubsub), to get a fast local tail from a kube (gke) cluster. 

First you want to create a log export to pubsub, and make note of the topic. It is assumed you know how to navigate your way around gcloud sdk and set your default credentials. 

Once you have logs flowing to your pubsub topic, say 'log-export-pubsub-topic-name', then you can run this app. 
It will create a subscription using the name specified in the config, trap SIGINT, and delete the subscription before exiting. 

To get up and running, add config file:
``` 
$ mkdir ~/.go-kube-tail
```

Add a file named config.json to ~/.go-kube-tail directory that looks something like this:
```
{
        "projectName": "my-cool-project",
        "topicName": "log-export-pubsub-topic-name
        "subscriptionName": "subscription-name-to-create"
}
```

Run the app with one or more of the following arguments:
```
$ ./go-kube-tail --help
  -C string
    	Specify config file, defaults to conf.json (default "/Users/rstrunk/.go-kube-tail/config.json")
  -c string
    	Specify container name, defaults to empty
  -n string
    	Specify namespace name, defaults to empty
  -p string
    	Specify pod regex (substring ok too)
```

E.g. to tail all container logs in all pods in the default namespace that have "fun" in their name...
```
$ ./go-kube-tail -n default -p fun
$ ./go-kube-tail -n default -p fun 
2017/09/20 23:29:25 Setting up logger...
2017/09/20 23:29:25 Topic: log-export-pubsub-topic-name
2017/09/20 23:29:25 Subscription: subscription-name-to-create
2017/09/20 23:29:25 Namespace: default
2017/09/20 23:29:25 Pod Name Regex: 'fun'
2017/09/20 23:29:29 Created subscription: projects/my-cool-project/subscriptions/subscription-name-to-create
2017-09-21T06:14:46Z [service-funny-1.4.1-153qd]: Received request {"method":"GET","url": "http://not-a-real-url"}
2017-09-21T06:14:46Z [funpod-0.25.0-zkz41]: 127.0.0.1 - - "GET /nginx_status/ HTTP/1.1" 200 790 "-" "Not a Real Agent/1.0" "-"
^C2017/09/20 23:29:34 interrupt
2017/09/20 23:29:34 Deleting subscription 'subscription-name-to-create'...
2017/09/20 23:29:34 Subscription 'subscription-name-to-create' deleted.
```
