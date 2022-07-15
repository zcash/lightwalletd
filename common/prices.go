package common

import (
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	// Map of all historical prices. Date as "yyyy-mm-dd" to price in USD
	historicalPrices map[string]float64 = make(map[string]float64)

	// The latest price
	latestPrice float64 = -1

	// Latest price was fetched at
	latestPriceTime time.Time

	// Mutex to guard both historical and latest price
	mutex sync.Mutex

	// Full path of the persistence file
	pricesFileName string
)

func fetchAPIPrice(url string, resultPath []string) (float64, error) {
	resp, err := http.Get(url)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	var priceJSON map[string]interface{}
	json.Unmarshal(body, &priceJSON)

	for i := 0; i < len(resultPath); i++ {
		d, ok := priceJSON[resultPath[i]]
		if !ok {
			return -1, fmt.Errorf("API error: couldn't find '%s'", resultPath[i])
		}

		switch v := d.(type) {
		case float64:
			return v, nil
		case string:
			{
				price, err := strconv.ParseFloat(v, 64)
				return price, err
			}

		case map[string]interface{}:
			priceJSON = v
		}

	}

	return -1, errors.New("path didn't result in lookup")
}

func fetchCoinbasePrice() (float64, error) {
	return fetchAPIPrice("https://api.coinbase.com/v2/exchange-rates?currency=ZEC", []string{"data", "rates", "USD"})
}

func fetchCoinCapPrice() (float64, error) {
	return fetchAPIPrice("https://api.coincap.io/v2/rates/zcash", []string{"data", "rateUsd"})
}

func fetchBinancePrice() (float64, error) {
	return fetchAPIPrice("https://api.binance.com/api/v3/avgPrice?symbol=ZECUSDC", []string{"price"})
}

func fetchCoingeckoPrice() (float64, error) {
	return fetchAPIPrice("https://api.coingecko.com/api/v3/coins/zcash", []string{"market_data", "current_price", "usd"})
}

func fetchHistoricalCoingeckoPrice(ts time.Time) (float64, error) {
	dt := ts.Format("02-01-2006") // dd-mm-yyyy
	url := fmt.Sprintf("https://api.coingecko.com/api/v3/coins/zcash/history?date=%s", dt)

	return fetchAPIPrice(url, []string{"market_data", "current_price", "usd"})
}

// calcMedian calculates the median of a sorted slice of numbers
func calcMedian(inp []float64) (median float64) {
	// For even numbers we add the two middle numbers
	// and divide by two using the mean function above
	// For odd numbers we just use the middle number
	n := len(inp)
	if n%2 == 0 {
		return (inp[n/2-1] + inp[n/2]) / 2
	} else {
		return inp[n/2]
	}
}

// fetchPriceFromWebAPI will fetch prices from multiple places, discard outliers and return the
// concensus price. This function doesn't need the mutex.
func fetchPriceFromWebAPI() (float64, error) {
	// We'll fetch prices from all our endpoints, and use the median price from that
	priceProviders := []func() (float64, error){
		fetchCoinbasePrice,
		fetchCoinCapPrice,
		fetchBinancePrice,
		fetchCoingeckoPrice,
	}

	// Get all prices
	prices := make([]float64, 0)
	for _, provider := range priceProviders {
		price, err := provider()
		if err == nil {
			Log.WithFields(logrus.Fields{
				"method":   "CurrentPrice",
				"provider": runtime.FuncForPC(reflect.ValueOf(provider).Pointer()).Name(),
				"price":    price,
			}).Info("Service")
			prices = append(prices, price)
		} else {
			Log.WithFields(logrus.Fields{
				"method":   "CurrentPrice",
				"provider": runtime.FuncForPC(reflect.ValueOf(provider).Pointer()).Name(),
				"error":    err,
			}).Error("Service")
		}
	}
	if len(prices) == 0 {
		return -1, errors.New("no price providers are available")
	}
	sort.Float64s(prices)

	// Get the median price
	median := calcMedian(prices)

	// Discard all values that are more than 20% outside the median
	validPrices := make([]float64, 0)
	for _, price := range prices {
		if (math.Abs(price-median) / median) > 0.2 {
			Log.WithFields(logrus.Fields{
				"method": "CurrentPrice",
				"error":  fmt.Sprintf("Discarding price (%.2f) because too far away from median (%.2f", price, median),
			}).Error("Service")
		} else {
			validPrices = append(validPrices, price)
		}
	}

	// At least 3 (valid) providers are required; note that if only 2 were required,
	// either could determine the price, since it could sample the other one.
	if len(validPrices) < 3 {
		return -1, errors.New("insufficient price providers are available")
	}

	// If we discarded too many, return an error
	if len(validPrices) < (len(prices)/2 + 1) {
		return -1, errors.New("not enough valid prices")
	}
	median = calcMedian(validPrices)
	if median <= 0 {
		return -1, errors.New("median price is <= 0")
	}
	return median, nil
}

func readHistoricalPricesFile() (map[string]float64, error) {
	f, err := os.Open(pricesFileName)
	if err != nil {
		Log.Errorf("Couldn't open file %s for writing: %v", pricesFileName, err)
		return nil, err
	}
	defer f.Close()

	j := gob.NewDecoder(f)
	var prices map[string]float64
	err = j.Decode(&prices)
	if err != nil {
		Log.Errorf("Couldn't decode historical prices: %v", err)
		return nil, err
	}

	Log.WithFields(logrus.Fields{
		"method":  "HistoricalPrice",
		"action":  "Read historical prices file",
		"records": len(prices),
	}).Info("Service")
	return prices, nil
}

func writeHistoricalPricesMap() {
	// Serialize the map to disk.
	f, err := os.OpenFile(pricesFileName, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		Log.Errorf("Couldn't open file %s for writing: %v", pricesFileName, err)
		return
	}
	defer f.Close()

	j := gob.NewEncoder(f)
	if err = j.Encode(historicalPrices); err != nil {
		Log.Errorf("Couldn't encode historical prices: %v", err)
		return
	}
	Log.WithFields(logrus.Fields{
		"method": "HistoricalPrice",
		"action": "Wrote historical prices file",
	}).Info("Service")
}

// GetCurrentPrice is a top-level API, returns the latest price that we
// have fetched if no more than 3 hours old, else an error. An error
// should not occur unless we can't reach enough price oracles.
func GetCurrentPrice() (float64, error) {
	if DarksideEnabled {
		return 222.333, nil
	}
	mutex.Lock()
	defer mutex.Unlock()

	if latestPriceTime.IsZero() {
		return -1, errors.New("starting up, prices not available yet")
	}

	// If the current price is too old, don't return it.
	if time.Since(latestPriceTime).Hours() > 3 {
		return -1, errors.New("price too old")
	}

	return latestPrice, nil
}

// return the time in YYYY-MM-DD string format
func day(t time.Time) string {
	return t.Format("2006-01-02")
}

// GetHistoricalPrice returns the price for the given day, but only
// accurate to day granularity.
func GetHistoricalPrice(ts time.Time) (float64, *time.Time, error) {
	if DarksideEnabled {
		return 333.444, &ts, nil
	}
	dt := day(ts)
	canonicalTime, err := time.Parse("2006-01-02", dt)
	if err != nil {
		return -1, nil, err
	}
	mutex.Lock()
	defer mutex.Unlock()
	if val, ok := historicalPrices[dt]; ok {
		return val, &canonicalTime, nil
	}
	// Check if this is the same as the current latest price
	if latestPrice > 0 && day(latestPriceTime) == dt {
		return latestPrice, &canonicalTime, nil
	}

	// Fetch price from web API
	mutex.Unlock()
	price, err := fetchHistoricalCoingeckoPrice(ts)
	mutex.Lock()
	if err != nil {
		Log.Errorf("Couldn't read historical prices from Coingecko: %v", err)
		return -1, nil, err
	}
	if price <= 0 {
		Log.Errorf("historical prices from Coingecko <= 0")
		return -1, nil, errors.New("bad Coingecko result")
	}
	// add to our cache so we don't have to hit Coingecko again
	// for the same date
	addHistoricalPrice(price, ts)

	return price, &canonicalTime, nil
}

// Add a price entry for the given day both to our map
// and to the file (so we'll have it after a restart).
// This caching allows us to hit coingecko less often,
// and provides resilience when that site is down.
//
// There are two ways a historical price can be added:
//   - When a client calls GetZECPrice to get a past price
//   - When a new day begins, we'll save the previous day's price
//
func addHistoricalPrice(price float64, ts time.Time) {
	dt := day(ts)
	if _, ok := historicalPrices[dt]; !ok {
		// an entry for this day doesn't exist, add it
		historicalPrices[dt] = price
		Log.WithFields(logrus.Fields{
			"method": "HistoricalPrice",
			"action": "Add",
			"date":   dt,
			"price":  price,
		}).Info("Service")
		writeHistoricalPricesMap()
	}
}

// StartPriceFetcher starts a new thread that will fetch historical and current prices
func StartPriceFetcher(dbPath string, chainName string) {
	// Set the prices file name
	pricesFileName = filepath.Join(dbPath, chainName, "prices")

	// Read the historical prices if available
	mutex.Lock()
	if prices, err := readHistoricalPricesFile(); err != nil {
		Log.Errorf("Couldn't read historical prices, starting with empty map")
	} else {
		historicalPrices = prices
		Log.Infof("prices at start: %v", prices)
	}
	mutex.Unlock()

	// Fetch the current price every 15 mins
	go func() {
		for {
			price, err := fetchPriceFromWebAPI()
			if err != nil {
				Log.Errorf("Error getting prices from web APIs: %v", err)
			} else {
				Log.WithFields(logrus.Fields{
					"method": "CurrentPrice",
					"price":  price,
				}).Info("Service")

				mutex.Lock()
				// If the day has changed, save the previous day's
				// historical price. Historical prices are per-day.
				if day(latestPriceTime) != day(time.Now()) {
					if latestPrice > 0 {
						t := time.Unix(latestPriceTime.Unix(), 0)
						addHistoricalPrice(latestPrice, t)
					}
				}
				latestPrice = price
				latestPriceTime = time.Now()
				mutex.Unlock()
			}
			// price data 15 minutes out of date is probably okay
			time.Sleep(15 * time.Minute)
		}
	}()
}
