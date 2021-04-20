package main

import (
	"context"
	"net/http"
	// "net/url"
	"strings"
	"os"
	"strconv"
	"errors"
	"github.com/google/go-github/v34/github" // with go modules enabled (GO111MODULE=on or outside GOPATH)
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
	"github.com/bndr/gojenkins"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"fmt"
	"io/ioutil"
	"regexp"
	"github.com/bradleyfalzon/ghinstallation"
)

type Command struct {
	Name string `yaml:"name"`
	Desc string `yaml:"description"`
	Context string `yaml:"context"`
	Parameters int `yaml:"parameters"`
	SuccessMessage string `yaml:"successMessage"`
	ErrorMessage string `yaml:"errorMessage"`
}

type Commands struct {
	List []Command `yaml:"command"`
	Prefix string `yaml:"prefix"`
}

type Jenkins struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	URL string `yaml:"url"`
}

type Github struct {
	Token string `yaml:"token"`
	AppId int64 `yaml:"appId"`
	AppInstallation int64 `yaml:"appInstallation"`
	AppPrivateKey string `yaml:"appPrivateKey"`
}

type Server struct {
	port string `yaml:"port"`
	address string `yaml:"address"`
}

type Logging struct {
	level string `yaml:"level"`
}

type Config struct {
	Github *Github `yaml:"github"`
	Commands *Commands `yaml:"commands"`
	Jenkins *Jenkins `yaml:"jenkins"`
	Server *Server `yaml:"server"`
	Logging *Logging `yaml:"logging"`
}

func (c *Commands) getCommand (name string) (*Command, error) {
	for _, command := range c.List {
		if(command.Name == name) {
			return &command, nil
		}
	}
	return nil, errors.New("command not found")
}

func (c *Command) execute(command []string, event *github.IssueCommentEvent, config *Config, client *github.Client) (error) {
	log.WithFields(log.Fields{
		"PR" : event.GetIssue().GetNumber(),
		"repo" : event.GetRepo().GetName(),
		"command" : command,
	}).Debug("received set command event")
	switch (c.Name) {
		case "set":
			jenkins, err := JenkinsClient(config.Jenkins.URL, config.Jenkins.Username, config.Jenkins.Password)
			if err != nil {
				return errors.New("error creating the client")
			}
			mainBuild, err := jenkins.GetJob(event.GetRepo().GetName())
			if err != nil {
				return errors.New("error getting the main jenkins job")
			}
			innerBuild, err := mainBuild.GetInnerJob("PR-" + strconv.Itoa(event.GetIssue().GetNumber()))
			if err != nil {
				log.Error(err)
				return errors.New("error getting the inner jenkins job")
			}
			params := map[string]string {
				command[1] : command[2],
			}
			_, err = innerBuild.InvokeSimple(params)
			if err != nil {
				return errors.New("error invoking build")
			}
			_, _, err = client.Issues.CreateComment(context.Background(), event.GetRepo().GetOwner().GetLogin(), event.GetRepo().GetName(), event.GetIssue().GetNumber(), &github.IssueComment{Body: &c.SuccessMessage})
			if err != nil {
				log.Error(err)
				return errors.New("error creating comment")
			}
			return nil
		case "lgtm":
				// submitReview(event, client)
			return nil
		case "close":
			// close the pull request automaically
			return nil
		default:
			return nil
	}
}

func JenkinsClient(url string, username string, password string) (*gojenkins.Jenkins, error) {
	jenkins := gojenkins.CreateJenkins(nil, url, username, password)
	_, err := jenkins.Init()
	if err != nil {
		return nil, errors.New("error creating a jenkins client")
	}
	return jenkins, nil
}

func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

// Find takes a slice and looks for an element in it. If found it will
// return it's key, otherwise it will return -1 and a bool of false.
func Find(slice []string, val string) (int, bool, error) {
    for i, item := range slice {
        if item == val {
            return i, true, nil
        }
    }
    return -1, false, errors.New("element not found in array")
}

func parseConfig(filename string) (*Config, error) {
	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	err = yaml.Unmarshal(file, config)
        if err != nil {
		 return nil, fmt.Errorf("in file %q: %v", file, err)
        }
	return config, nil
}

func init() {
	if err := godotenv.Load(); err != nil {
		log.Error(".env file found")
	}
    lvl, ok := os.LookupEnv("LOG_LEVEL")
    if !ok {
        lvl = "debug"
    }
    ll, err := log.ParseLevel(lvl)
    if err != nil {
        ll = log.DebugLevel
    }
    log.SetLevel(ll)
}

func handleComments(event *github.IssueCommentEvent, client *github.Client, c *Config) (error) {
	commandStr := event.GetComment().GetBody()
	if event.GetIssue().IsPullRequest() {
		if strings.HasPrefix(commandStr, c.Commands.Prefix) {

			commandStr = strings.TrimPrefix(commandStr, c.Commands.Prefix)
			commandArray := strings.Split(commandStr, " ")

			command, err := c.Commands.getCommand(commandArray[0])
			if err != nil {
				return errors.New("invalid command, please view the command list")
			}

			if command.Parameters != (len(commandArray)-1) {
				return errors.New("invalid number of parameters, please run help command")
			}

			err = command.execute(commandArray, event, c, client)
			if err != nil {
				log.Error(err)
				return errors.New("error executing a command")
			}
		}
	}
	return nil
}

func validateRegex(text string, regex string)(bool){
	var valid = regexp.MustCompile(regex)
	return valid.MatchString(text)
}

func validateBody(text string, regex string)(bool){
	var valid = regexp.MustCompile(regex)
	substring := valid.FindStringSubmatch(text)
	if len(substring) > 0 {
		description := strings.TrimSpace(valid.FindStringSubmatch(text)[1])
		if len(description) > 0 && description != "Please describe your pull request."{
			return true
		}
	}
	return false
}

func createCheck(client *github.Client, owner string, repo string, sha, name string, valid bool){
	var conclusion = "skipped"
	if valid == true {
		conclusion = "success"
	}else{
		conclusion = "failure"
	}

	opts := github.CreateCheckRunOptions{
		Name:       name,
		HeadSHA:    sha,
		Status:     github.String("completed"),
		Conclusion: github.String(conclusion),
	 }

	 _, _, err := client.Checks.CreateCheckRun(context.Background(), owner, repo, opts)
	 if err != nil {
		log.Error(err)
	}
}

func handlePullRequest(event *github.PullRequestEvent, client *github.Client, c *Config) (error) {
	var (
		title = event.GetPullRequest().GetTitle()
		body = event.GetPullRequest().GetBody()
		owner = event.GetRepo().GetOwner().GetLogin()
		repo = event.GetRepo().GetName()
		sha = event.GetPullRequest().Head.GetSHA()
		regexTitle = `^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)(\([a-z ]+\))?(!)?: [\w ]+$`
		regexBody = `(?s)## Description(.*)## Other information`
	)

	isValidTitle := validateRegex(title, regexTitle)
	createCheck(client, owner, repo, sha, "Conventional Commits", isValidTitle)

	isValidBody := validateBody(body,regexBody)
	createCheck(client, owner, repo, sha, "Description Pull Request", isValidBody)

	return nil 
}

func newClient(githubToken string) (*github.Client){
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(context.Background(), ts)
	return github.NewClient(tc)
}

func newAppClient(githubConfig *Github)(*github.Client){
	// Shared transport to reuse TCP connections.
	tr := http.DefaultTransport
	key := []byte(githubConfig.AppPrivateKey)

	// Wrap the shared transport for use with the app ID authenticating with installation ID.
	itr, err := ghinstallation.New(tr, githubConfig.AppId, githubConfig.AppInstallation, key)
	if err != nil {
		log.Fatal(err)
	}

	// Use installation transport with github.com/google/go-github
	return github.NewClient(&http.Client{Transport: itr})
}

type webhookHandler struct {
	Config *Config
}

func (wh *webhookHandler) handler(w http.ResponseWriter, r *http.Request) {
	githubClient := newClient(wh.Config.Github.Token)
	githubAppClient := newAppClient(wh.Config.Github)
	payload, err := github.ValidatePayload(r, []byte(""))
	if err != nil {
		log.Error("error validating request body: err=%s\n", err)
		return
	}
	defer r.Body.Close()
	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		log.Error("could not parse webhook: err=%s\n", err)
		return
	}
	switch e := event.(type) {
		case *github.IssueCommentEvent:
			err := handleComments(e, githubClient, wh.Config)
			if err != nil {
				log.Error(err)
				return
			}
        case *github.PullRequestEvent:
            err := handlePullRequest(e, githubAppClient, wh.Config)
            if err != nil {
                log.Error(err)
                return
            }
		default:
			return
	}
}

func main() {
	config, err := parseConfig("examples/config.yaml")
    if err != nil {
		log.Fatal(err)
    }
	log.Info("server listening on 0.0.0.0:8080")
	webhookHandlerMain := &webhookHandler{Config: config}
	http.HandleFunc("/webhook", webhookHandlerMain.handler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
