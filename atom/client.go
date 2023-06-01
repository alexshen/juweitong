package atom

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
	"github.com/samber/lo"
)

const (
	kDomain         = "www.juweitong.cn"
	kBaseUrl        = "https://" + kDomain + "/neighbour"
	kClientProtocol = "2.1"
)

const (
	kStateLoggedOut = iota
	kStateScanQRCode
	kStateLoggedIn
)

var (
	ErrQRLoginAlreadyStarted = errors.New("qr login already started")
	ErrNotLoggedIn           = errors.New("not logged in")
)

type Community struct {
	Name     string `json:"value"` // name of the community
	MemberId string `json:"key"`   // member id in this community
}

type LikedPost struct {
	MemberId string
	PostId   string
}

type LikedPostsHistory interface {
	Has(post LikedPost) (bool, error)
	Add(post LikedPost) error
}

type NullLikedPostsHistory struct{}

func (o NullLikedPostsHistory) Has(post LikedPost) (bool, error) {
	return false, nil
}

func (o NullLikedPostsHistory) Add(post LikedPost) error {
	return nil
}

type Client struct {
	communities  []Community
	state        atomic.Int32
	httpclient   *resty.Client
	loginConn    *websocket.Conn
	loginDone    chan struct{}
	curCommunity int
	history      LikedPostsHistory
}

type negotiationResult struct {
	ConnectionToken string
	ConnectionId    string
}

type LoginHandler func()

type likePostConfig struct {
	viewPostApiPath string
	listPostApiPath string
	favText         string            // text of the like button
	listPostParams  map[string]string // query params for getting list of posts
}

var (
	noticeConfig = likePostConfig{
		"/community/title_view?title=",
		"/community/notice_list_more",
		"点赞",
		map[string]string{"condtion": `{"sortCondition":"1","partCondition":""}`},
	}
	momentsConfig = likePostConfig{
		"/community/around_view?title=",
		"/community/around_help_list_more",
		"点赞",
		map[string]string{"condition": `{"tag":"","little":"","sortCondition":""}`},
	}
	ccpNoticeConfig = likePostConfig{
		"/community/ccp_view?title=",
		"/community/ccp_list_more",
		"点赞",
		map[string]string{
			"category":  "80",
			"condition": "{}",
		},
	}
	proposalConfig = likePostConfig{
		"/community/proposal_view?caseId=",
		"/community/proposal_list_more",
		"赞成",
		map[string]string{"condition": "{}"},
	}
)

func Get(req *resty.Request, url string) (*resty.Response, error) {
	resp, err := req.Get(url)
	if err == nil && !resp.IsSuccess() {
		path, _ := strings.CutPrefix(resp.Request.URL, kBaseUrl)
		err = fmt.Errorf("%s: %s", path, resp.Status())
	}
	return resp, nil
}

// NewClient creates a client with the DAO. A DAO is used for speeding up
// the liking process by ignoring already liked posts.
func NewClient(history LikedPostsHistory) *Client {
	c := &Client{
		httpclient:   resty.New(),
		curCommunity: -1,
		history:      history,
	}
	c.httpclient.SetBaseURL(kBaseUrl)
	return c
}

func (cli *Client) SetTimeout(d time.Duration) {
	cli.httpclient.SetTimeout(d)
}

// StartQRLogin starts the qr login process and returns the url of the qr code.
// If the login already started, ErrQRLoginAlreadyStarted is returned
func (cli *Client) StartQRLogin(onLogin LoginHandler) (string, error) {
	if cli.state.Load() == kStateScanQRCode {
		return "", ErrQRLoginAlreadyStarted
	}

	negot, err := cli.negotiate()
	if err != nil {
		return "", err
	}

	conn, err := cli.createLoginConnection(negot.ConnectionToken)
	if err != nil {
		return "", err
	}

	cli.loginConn = conn
	return cli.doQRLogin(negot.ConnectionId, onLogin)
}

func (cli *Client) negotiate() (negotiationResult, error) {
	var negot negotiationResult
	_, err := Get(
		cli.httpclient.R().
			SetQueryParams(map[string]string{
				"clientProtocol": kClientProtocol,
				"_":              strconv.FormatInt(time.Now().UnixMilli(), 10),
			}).
			SetResult(&negot),
		"/authorize/negotiate")
	return negot, err
}

func (cli *Client) createLoginConnection(token string) (*websocket.Conn, error) {
	// start the websocket connection
	opts := url.Values{}
	opts.Set("clientProtocol", "2.1")
	opts.Set("transport", "webSockets")
	opts.Set("connectionToken", token)
	opts.Set("tid", strconv.Itoa(int(rand.Float32()*11)))

	u := "wss://" + kDomain + "/neighbour/authorize/connect?" + opts.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u, nil)
	if err != nil {
		return nil, err
	}

	opts.Del("tid")
	opts.Add("_", strconv.FormatInt(time.Now().UnixMilli(), 10))

	_, err = Get(
		cli.httpclient.R(),
		"/authorize/start?"+opts.Encode())
	if err != nil {
		conn.Close()
		conn = nil
	}

	return conn, err
}

func (cli *Client) doQRLogin(id string, onLogin LoginHandler) (string, error) {
	type qrcodeResponse struct {
		err error
		url string
	}
	cli.loginDone = make(chan struct{})
	initDone := make(chan qrcodeResponse)

	go func() {
		err := cli.loginConn.WriteMessage(websocket.TextMessage, []byte("qr"))
		if err != nil {
			initDone <- qrcodeResponse{err: err}
			return
		}

		type message struct {
			Init     bool   `json:"init"`
			BindUser bool   `json:"bindUser"`
			Id       string `json:"id"`
			Value    string `json:"value"`
		}
		type response struct {
			M []message
		}
		var scanning bool
		for {
			var resp response
			err := cli.loginConn.ReadJSON(&resp)
			if err != nil {
				if !scanning {
					initDone <- qrcodeResponse{err: err}
				} else {
					break
				}
			}
			if len(resp.M) == 0 {
				continue
			}
			if resp.M[0].Init {
				resp, err := Get(
					cli.httpclient.R().SetQueryParam("id", id),
					"/home/qr_login_more_v1")
				if err != nil {
					initDone <- qrcodeResponse{err: err}
					break
				}

				cli.state.Store(kStateScanQRCode)
				scanning = true
				initDone <- qrcodeResponse{
					url: regexp.MustCompile("\"([^\"]+)").FindStringSubmatch(resp.String())[1],
				}
			} else if resp.M[0].BindUser {
				_, err := Get(
					cli.httpclient.R().SetQueryParam("id", id),
					"/home/qr_login_do")
				if err != nil {
					log.Printf("qr_login_do: %v", err)
					cli.state.Store(kStateLoggedOut)
				} else {
					cli.updateCommunities()
					cli.updateCurrentCommunity()
					cli.state.Store(kStateLoggedIn)
					if onLogin != nil {
						onLogin()
					}
				}
				break
			}
		}

		cli.loginConn.Close()
		close(cli.loginDone)
	}()

	res := <-initDone
	return res.url, res.err
}

func (cli *Client) updateCommunities() {
	type binding struct {
		CommunityName string `json:"community_name"`
		Status        string `json:"status"`
		Member        string `json:"member"`
	}
	var res struct {
		Binds []binding `json:"binds"`
	}
	_, err := Get(
		cli.httpclient.R().
			SetQueryParam("seed", strconv.FormatInt(time.Now().UnixMilli(), 10)).
			SetQueryParam("wxid", "").
			SetResult(&res),
		"/api/register/member/bind")
	if err != nil {
		log.Println(err)
		return
	}
	cli.communities = lo.FilterMap(res.Binds, func(e binding, i int) (Community, bool) {
		if e.Status != "已通过" {
			return Community{}, false
		}
		return Community{e.CommunityName, e.Member}, true
	})

}

func (cli *Client) updateCurrentCommunity() {
	resp, err := Get(cli.httpclient.R(), "/home/home")
	if err != nil {
		log.Print(err)
		return
	}
	if !resp.IsSuccess() {
		log.Printf("get home: %s", resp.Status())
		return
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		log.Print(err)
		return
	}

	curCommName := doc.Find("#changeMember span").First().Text()
	_, cli.curCommunity, _ = lo.FindIndexOf(cli.communities, func(e Community) bool {
		return e.Name == curCommName
	})
}

func (cli *Client) StopQRLogin() {
	if cli.state.Load() != kStateScanQRCode {
		return
	}

	cli.loginConn.SetReadDeadline(time.Now())
	<-cli.loginDone
}

func (cli *Client) IsLoggedIn() bool {
	return cli.state.Load() == kStateLoggedIn
}

func (cli *Client) Communities() []Community {
	return cli.communities
}

// SetCurrentCommunity sets the current community at the given index
func (cli *Client) SetCurrentCommunity(i int) error {
	if err := cli.ensureLoggedIn(); err != nil {
		return err
	}
	_, err := Get(cli.httpclient.R().
		SetQueryParam("seed", strconv.FormatInt(time.Now().UnixMilli(), 10)),
		"/api/member/switch/"+cli.communities[i].MemberId)
	if err != nil {
		return err
	}
	cli.curCommunity = i
	return nil
}

func (cli *Client) CurrentCommunityIndex() int {
	return cli.curCommunity
}

func (cli *Client) CurrentCommunity() Community {
	if cli.curCommunity == -1 {
		return Community{}
	}
	return cli.communities[cli.curCommunity]
}

// LikeNotices visits count of the latest notices and returns the number of
// posts that have been liked
func (cli *Client) LikeNotices(count int) int {
	if err := cli.ensureLoggedIn(); err != nil {
		return 0
	}

	ids, err := cli.getPostIds(
		noticeConfig.listPostApiPath,
		noticeConfig.listPostParams,
		count)
	if err != nil {
		log.Print(err)
		return 0
	}
	return cli.likePosts(ids, noticeConfig)
}

func (cli *Client) LikeMoments(count int) int {
	if err := cli.ensureLoggedIn(); err != nil {
		return 0
	}

	ids, err := cli.getPostIds(
		momentsConfig.listPostApiPath,
		momentsConfig.listPostParams,
		count)
	if err != nil {
		log.Print(err)
		return 0
	}
	return cli.likePosts(ids, momentsConfig)
}

func (cli *Client) LikeCCPPosts(count int) int {
	if err := cli.ensureLoggedIn(); err != nil {
		return 0
	}

	ids, err := cli.getPostIds(
		ccpNoticeConfig.listPostApiPath,
		ccpNoticeConfig.listPostParams,
		count)
	if err != nil {
		log.Print(err)
		return 0
	}
	return cli.likePosts(ids, ccpNoticeConfig)
}

func (cli *Client) LikeProposals(count int) int {
	if err := cli.ensureLoggedIn(); err != nil {
		return 0
	}

	ids, err := cli.getPostIds(
		proposalConfig.listPostApiPath,
		proposalConfig.listPostParams,
		count)
	if err != nil {
		log.Print(err)
		return 0
	}
	return cli.likePosts(ids, proposalConfig)
}

func (cli *Client) likePosts(ids []string, config likePostConfig) int {
	communityId := cli.CurrentCommunity().MemberId
	newIds := lo.Filter(ids, func(id string, i int) bool {
		res, err := cli.history.Has(LikedPost{communityId, id})
		if err != nil {
			log.Printf("failed to check liked post: %v", err)
			return false
		}
		return !res
	})
	wg := sync.WaitGroup{}
	n := atomic.Int32{}
	for _, id := range newIds {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			liked, err := cli.likePost(config.viewPostApiPath, config.favText, id)
			if err != nil {
				log.Print(err)
			} else {
				if err := cli.history.Add(LikedPost{communityId, id}); err != nil {
					log.Printf("failed to add liked post: %v", err)
					return
				}
				if liked {
					n.Add(1)
				}
			}
		}(id)
	}
	wg.Wait()
	return int(n.Load())
}

// getPostIds returns count of ids of the latest notices
func (cli *Client) getPostIds(apiPath string, queryParams map[string]string, count int) ([]string, error) {
	resp, err := Get(
		cli.httpclient.R().
			SetQueryParams(queryParams).
			SetQueryParam("begin", "0").
			SetQueryParam("count", strconv.Itoa(count)),
		apiPath)

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if err != nil {
		return nil, err
	}
	return doc.Find("body > div > a").Map(func(i int, e *goquery.Selection) string {
		value, _ := e.Attr("href")
		return value[strings.IndexRune(value, '=')+1 : strings.LastIndex(value, "'")]
	}), nil
}

func (cli *Client) likePost(apiPath string, favText string, id string) (bool, error) {
	resp, err := Get(cli.httpclient.R(), apiPath+id)
	if err != nil {
		return false, err
	}

	// only like when the post has not been liked
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(resp.String()))
	if doc.Find("span#cmdLike").First().Text() != favText {
		return false, nil
	}

	_, err = Get(cli.httpclient.R().SetQueryParam("title", id), "/community/title_like")
	return err == nil, err
}

func (cli *Client) ensureLoggedIn() error {
	if cli.state.Load() != kStateLoggedIn {
		return ErrNotLoggedIn
	}
	return nil
}
