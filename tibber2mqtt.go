package main

import (
        "log"
        "os"
//      "encoding/json"
        "fmt"
        "io"
        "strings"
        "github.com/spf13/viper"
        "github.com/natefinch/lumberjack"
        "github.com/go-resty/resty/v2"
        "github.com/romshark/jscan"
        mqtt "github.com/eclipse/paho.mqtt.golang"
)

var do_trace bool = true

var ownlog string
var tibberurl string
var tibbertoken string

var ownlogger io.Writer

var broker = "192.168.0.211"
var port = 1883

var mclient mqtt.Client
var opts = mqtt.NewClientOptions()

func main() {
// Set location of config 
        viper.SetConfigName("tibber2mqtt") // name of config file (without extension)
        viper.AddConfigPath("/etc/")   // path to look for the config file in

// Read config
        read_config()

        opts.AddBroker(fmt.Sprintf("tcp://%s:%d", broker, port))
        opts.SetClientID("tibber2mqtt")
//      opts.SetUsername("emqx")
//      opts.SetPassword("public")
        opts.SetDefaultPublishHandler(messagePubHandler)
        opts.OnConnect = connectHandler
        opts.OnConnectionLost = connectLostHandler
        mclient = mqtt.NewClient(opts)
        if token := mclient.Connect(); token.Wait() && token.Error() != nil {
                panic(token.Error())
        }

// Get commandline args
        if len(os.Args) > 1 {
                a1 := os.Args[1]
                if a1 == "readPrices" {
                        getTibber()
                        os.Exit(0)
                }
                fmt.Println("parameter invalid")
                os.Exit(-1)
        }
        if len(os.Args) == 1 {
                myUsage()
        }
}

func read_config() {
        err := viper.ReadInConfig() // Find and read the config file
        if err != nil { // Handle errors reading the config file
                log.Fatalf("Config file not found: %v", err)
        }

        ownlog = viper.GetString("own_log")
        if ownlog =="" { // Handle errors reading the config file
                log.Fatalf("Filename for ownlog unknown: %v", err)
        }
// Open log file
        ownlogger = &lumberjack.Logger{
                Filename:   ownlog,
                MaxSize:    5, // megabytes
                MaxBackups: 3,
                MaxAge:     28, //days
                Compress:   true, // disabled by default
        }
//        defer ownlogger.Close()
        log.SetOutput(ownlogger)

        tibberurl = viper.GetString("tibberurl")
        tibbertoken = viper.GetString("tibbertoken")

        if do_trace {
                log.Println("do_trace: ",do_trace)
                log.Println("own_log; ",ownlog)
        }
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
    fmt.Printf("Connected to MQTT server %s\n",broker)
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
    fmt.Printf("Connect lost: %v", err)
}

func myUsage() {
     fmt.Printf("Usage: %s argument\n", os.Args[0])
     fmt.Println("Arguments:")
     fmt.Println("backup        Backup the directories mentioned in the config file")
     fmt.Println("list          List all backups")
     fmt.Println("fetch         Fetch backup from server")
     fmt.Println("decrypt       Decrypt backup")
}

func getTibber() {
        var tibberquery string = `{ "query": "{viewer {homes {currentSubscription {priceInfo {current {total startsAt} today {total startsAt} tomorrow {total startsAt}}}}}}"}`
        var total string

        token := mclient.Publish("topic/out/state", 0, false, "on")
        token.Wait()

        // Create a Resty Client
        client := resty.New()

        // POST JSON string
        // No need to set content type, if you have client level setting
        resp, err := client.R().
                SetHeader("Content-Type", "application/json").
                SetBody(tibberquery).
                SetAuthToken(tibbertoken).
                Post(tibberurl)
        if err == nil {
                err = jscan.Scan(jscan.Options{
                        CachePath:  true,
                        EscapePath: true,
                }, string(resp.Body()), func(i *jscan.Iterator) (err bool) {
                        if i.Key() == "total" {
                                total = i.Value();
                        }
                        if i.Key() == "startsAt" {
                                var topic string = "topic/out/"
                                var temp string
                                temp = topic + "total" + i.Value()[11:13]
                                if strings.Contains(i.Path(), "tomorrow"){
                                        temp = topic + "tomorrow" + i.Value()[11:13]
                                } 
                                fmt.Printf("At %s, total %s \n", i.Value(), total)
                                token = mclient.Publish(temp, 0, false, total)
                                token.Wait()
                        }
                        return false // No Error, resume scanning
                })
        } else {
                fmt.Println(err)
        }
}
