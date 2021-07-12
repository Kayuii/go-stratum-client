package main

import (
	"io/ioutil"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"go-stratum-client"
)

var testConfig map[string]interface{}

func init() {
	log.SetLevel(log.DebugLevel)

	b, err := ioutil.ReadFile("test-config.yaml")
	if err != nil {
		log.Errorf("No test-config.yaml")
		str := `pool:
username:
pass:
`
		if err := ioutil.WriteFile("test-config.yaml", []byte(str), 0666); err != nil {
			log.Errorf("Failed to create test-config.yaml: %v", err)
		} else {
			log.Infof("Created test-config.yaml..run tests after filling it out")
			os.Exit(-1)
		}
	} else {
		if err := yaml.Unmarshal(b, &testConfig); err != nil {
			log.Fatalf("Failed to unmarshal test-config.yaml: %v", err)
		}
	}
	// os.Exit(m.Run())
}

func connect(sc *stratum.StratumContext) error {
	err := sc.Connect(testConfig["pool"].(string))
	if err != nil {
		log.Debugf("Connected to pool..")
	}
	return err
}

func main() {

	sc := stratum.New()
	err := connect(sc)
	if err != nil {
		panic(err)
	}

	wg := sync.WaitGroup{}
	wg.Add(2)

	workChan := make(chan *stratum.Work)
	sc.RegisterWorkListener(workChan)

	go func() {
		for _ = range workChan {
			log.Debugf("Calling wg.Done()")
			wg.Done()
		}
	}()

	err = sc.Authorize(testConfig["username"].(string), testConfig["pass"].(string))
	if err != nil {
		panic(err)
	}

	wg.Wait()
}
