package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	slackbot "github.com/adampointer/go-slackbot"
	"github.com/essentialkaos/slack"
	_ "github.com/lib/pq"
)

const (
	apiEndpoint = "https://api.coinmarketcap.com/v1/ticker/"
)

var client *http.Client
var db *sql.DB
var conf botConfig
var confPath = os.Getenv("HOME") + "/.aws_conf/yachtbot.config"

func main() {
	bot := slackbot.New(conf.Slack.Token)
	toMe := bot.Messages(slackbot.DirectMessage, slackbot.DirectMention).Subrouter()
	toMe.Hear("").MessageHandler(queryHandler)
	bot.Run()
}

func init() {
	client = &http.Client{Timeout: time.Second * 10}

	_, err := toml.DecodeFile(confPath, &conf)
	if err != nil {
		panic(err)
	}

	dbinfo := fmt.Sprintf("user=%s password=%s dbname=%s host=%s port=%s sslmode=disable",
		conf.Db.User, conf.Db.Pw, conf.Db.Name, conf.Db.Endpoint, conf.Db.Port)

	db, err = sql.Open("postgres", dbinfo)
	if err != nil {
		panic(err)
	}
}

func queryHandler(ctx context.Context, bot *slackbot.Bot, evt *slack.MessageEvent) {
	tickerSplit := strings.Split(evt.Msg.Text, " ")
	fmt.Println(tickerSplit)

	ticker := strings.ToUpper(tickerSplit[len(tickerSplit)-1])

	// Easter eggs
	switch ticker {
	case "XVG":
		bot.Reply(evt, ":joy::joy::joy:", slackbot.WithTyping)
		return
	case "USD":
		bot.Reply(evt, ":trash:", slackbot.WithTyping)
		return
	}

	attachment, err := getSingle(ticker)
	if err != nil {
		fmt.Println(err)
		return
	}

	attachments := []slack.Attachment{attachment}
	bot.ReplyWithAttachments(evt, attachments, slackbot.WithTyping)
}

func getSingle(ticker string) (slack.Attachment, error) {
	id, err := getID(db, ticker)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle getID: %v", err)
	}

	if id == "" {
		return slack.Attachment{}, fmt.Errorf("\n getSingle null ID: %v", err)
	}

	target := apiEndpoint + id

	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle req: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return slack.Attachment{}, fmt.Errorf("\n Bad response: %s", resp.Status)
	}

	payload := make([]Response, 0)
	err = json.NewDecoder(resp.Body).Decode(&payload)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle Decode: %v", err)
	}

	// No financial decisions better be made out of this
	priceUSD, err := strconv.ParseFloat(payload[0].PriceUSD, 64)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle ParseFloat: %v", err)
	}

	pct24h, err := strconv.ParseFloat(payload[0].Change24h, 64)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle ParseFloat: %v", err)
	}
	diff24h := priceUSD - (priceUSD / (pct24h + 1))

	pct7d, err := strconv.ParseFloat(payload[0].Change7d, 64)
	if err != nil {
		return slack.Attachment{}, fmt.Errorf("\n getSingle ParseFloat: %v", err)
	}
	diff7d := priceUSD - (priceUSD / (pct7d + 1))

	color, emoji := getReaction(pct24h)

	attachment := slack.Attachment{
		Title:     fmt.Sprintf("Price of %s - $%s %s", payload[0].Name, payload[0].Symbol, emoji),
		TitleLink: fmt.Sprintf("https://coinmarketcap.com/currencies/%s/", id),
		Fallback:  "Cryptocurrency Price",
		Color:     color,
		Fields: []slack.AttachmentField{
			slack.AttachmentField{
				Title: "Price USD",
				Value: fmt.Sprintf("$%.2f", priceUSD),
				Short: true,
			},
			slack.AttachmentField{
				Title: "Price BTC",
				Value: payload[0].PriceBTC,
				Short: true,
			},
			slack.AttachmentField{
				Title: "24H Change",
				Value: fmt.Sprintf("%s (%s%%)", currency(diff24h), payload[0].Change24h),
				Short: true,
			},
			slack.AttachmentField{
				Title: "7D Change",
				Value: fmt.Sprintf("%s (%s%%)", currency(diff7d), payload[0].Change7d),
				Short: true,
			},
		},
		Footer: "ESKETIT",
	}

	return attachment, nil
}

func getID(db *sql.DB, ticker string) (string, error) {
	cleanTicker := strings.Replace(ticker, "$", "", -1)

	stmt, err := db.Prepare(fmt.Sprintf("SELECT id FROM %s WHERE ticker = $1;", conf.Db.Table))
	if err != nil {
		return "", fmt.Errorf("\n getSingle db.Prepare: %v", err)
	}

	var id string
	rows, err := stmt.Query(cleanTicker)
	if err != nil {
		return "", fmt.Errorf("\n getSingle query: %v", err)
	}

	for rows.Next() {
		err = rows.Scan(&id)
		if err != nil {
			return "", fmt.Errorf("\n getSingle scan: %v", err)
		}
	}

	return id, nil
}

func getReaction(pct24h float64) (string, string) {
	switch {
	case pct24h < -50:
		return "#d7191c", ":trash::fire:"
	case pct24h < -25:
		return "#d7191c", ":smoking:"
	case pct24h < -10:
		return "#fdae61", ":thinking_face:"
	case pct24h < 0:
		return "#FAD898", ":zzz:"
	case pct24h < 25:
		return "#FAD898", ":beers:"
	case pct24h < 50:
		return "#a6d96a", ":champagne:"
	case pct24h < 100:
		return "#1a9641", ":racing_car:"
	case pct24h < 1000:
		return "#1a9641", ":motor_boat:"
	default:
		return "#000000", ":full_moon_with_face:"
	}
}

type currency float64

func (c currency) String() string {
	if c < 0 {
		return fmt.Sprintf("-$%.2f", math.Abs(float64(c)))
	}
	return fmt.Sprintf("$%.2f", float32(c))
}

// Response from CoinMarketCap API
type Response struct {
	ID              string `json:"id,omitempty"`
	Name            string `json:"name,omitempty"`
	Symbol          string `json:"symbol,omitempty"`
	Rank            string `json:"rank,omitempty"`
	PriceUSD        string `json:"price_usd,omitempty"`
	PriceBTC        string `json:"price_btc,omitempty"`
	Volume24h       string `json:"24h_volume_usd,omitempty"`
	MarketCap       string `json:"market_cap_usd,omitempty"`
	SupplyAvailable string `json:"available_supply,omitempty"`
	SupplyTotal     string `json:"total_supply,omitempty"`
	SupplyMax       string `json:"max_supply,omitempty"`
	Change1h        string `json:"percent_change_1h,omitempty"`
	Change24h       string `json:"percent_change_24h,omitempty"`
	Change7d        string `json:"percent_change_7d,omitempty"`
	Updated         string `json:"last_updated,omitempty"`
}

type botConfig struct {
	Db    dbConfig
	Slack slackConfig
}

type dbConfig struct {
	Endpoint string
	Port     string
	Name     string
	Table    string
	User     string
	Pw       string
}

type slackConfig struct {
	Token string
}
