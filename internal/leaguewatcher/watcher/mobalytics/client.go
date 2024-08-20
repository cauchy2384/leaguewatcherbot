package mobalytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"leaguewatcher/internal/leaguewatcher"
	"net/http"
	"strings"
	"time"

	"github.com/hasura/go-graphql-client"
	"go.uber.org/zap"
)

type Client struct {
	client *http.Client
	gql    *graphql.Client
	champs map[int]leaguewatcher.Champion
	logger *zap.Logger
}

func NewClient(logger *zap.Logger) *Client {
	client := http.Client{
		Timeout: 30 * time.Second,
	}

	return &Client{
		client: &client,
		gql:    graphql.NewClient(mobaAPI, &client),
		champs: make(map[int]leaguewatcher.Champion),
		logger: logger,
	}
}

const (
	mobaAPI = `https://app.mobalytics.gg/api/lol/graphql/v1/query`
	mobaWS  = `wss://ws.mobalytics.gg/api/lol/graphql/v1/query`
)

const queryMatches = `query LolProfilePageQueryMatchesShort($region: Region!, $gameName: String!, $tagLine: String!, $top: Int!) {
	lol {
		player(region: $region, gameName: $gameName, tagLine: $tagLine) {
			name
			region
			matchesHistory(top: $top, skip: 0) {
				matches {
					id
					startedAt
					duration
					queue
					teams {
						id
						result
					}
					subject {
						gameName
						tagLine
						region
						team
						championId
						role
						kda {
							kills
							deaths
							assists
						}
						cs
						lp {
							lpDiff
						}
					}
				}
			}
		}
	}
}`

func (c *Client) Matches(ctx context.Context, region, summoner, tag string,
) ([]leaguewatcher.Match, error) {

	type Request struct {
		Query     string `json:"query"`
		Variables struct {
			Region   string `json:"region"`
			GameName string `json:"gameName"`
			Tag      string `json:"tagLine"`
			Top      int    `json:"top"`
		} `json:"variables"`
	}

	req := Request{
		Query: queryMatches,
		Variables: struct {
			Region   string `json:"region"`
			GameName string `json:"gameName"`
			Tag      string `json:"tagLine"`
			Top      int    `json:"top"`
		}{
			Region:   strings.ToUpper(region),
			GameName: strings.ToLower(summoner),
			Tag:      strings.ToLower(tag),
			Top:      1,
		},
	}

	buf := bytes.NewBuffer(nil)
	err := json.NewEncoder(buf).Encode(req)
	if err != nil {
		return nil, err
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, mobaAPI, buf)
	if err != nil {
		return nil, err
	}
	r.Header.Add("Content-Type", "application/json")

	resp, err := c.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	type Response struct {
		Data struct {
			Lol struct {
				Player struct {
					MatchesHistory struct {
						Matches []struct {
							Id       int    `json:"id"`
							StaredAt int    `json:"startedAt"`
							Duration int    `json:"duration"`
							Queue    string `json:"queue"`
							Teams    []struct {
								Id     string `json:"id"`
								Result string `json:"result"`
							} `json:"teams"`
							Subject struct {
								GameName   string `json:"gameName"`
								Region     string `json:"region"`
								Tag        string `json:"tagLine"`
								Team       string `json:"team"`
								ChampionId int    `json:"championId"`
								Role       string `json:"role"`
								Kda        struct {
									Kills   int `json:"kills"`
									Deaths  int `json:"deaths"`
									Assists int `json:"assists"`
								} `json:"kda"`
								Cs int `json:"cs"`
								Lp struct {
									LpDiff *int `json:"lpDiff"`
								} `json:"lp"`
							} `json:"subject"`
						} `json:"matches"`
					} `json:"matchesHistory"`
				} `json:"player"`
			} `json:"lol"`
		} `json:"data"`
	}

	var respData Response
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		return nil, err
	}

	var matches []leaguewatcher.Match
	for _, match := range respData.Data.Lol.Player.MatchesHistory.Matches {

		var teamWon string
		for _, t := range match.Teams {
			if t.Result == "WON" {
				teamWon = t.Id
			}
		}

		var queue leaguewatcher.Queue
		switch match.Queue {
		case "NORMAL_DRAFT":
			queue = leaguewatcher.QueueNormalDraft
		case "RANKED_SOLO":
			queue = leaguewatcher.QueueRankedSolo
		case "RANKED_FLEX":
			queue = leaguewatcher.QueueRankedFlex
		case "ARAM":
			queue = leaguewatcher.QueueARAM
		}

		m := leaguewatcher.Match{
			ID:        match.Id,
			StartedAt: time.Unix(int64(match.StaredAt), 0),
			Duration:  match.Duration,

			Player: leaguewatcher.Player{
				Region: strings.ToLower(match.Subject.Region),
				Name:   strings.ToLower(match.Subject.GameName),
				Tag:    strings.ToLower(match.Subject.Tag),
			},
			Queue: queue,
			Role:  match.Subject.Role,

			Win: match.Subject.Team == teamWon,

			Kills:   match.Subject.Kda.Kills,
			Deaths:  match.Subject.Kda.Deaths,
			Assists: match.Subject.Kda.Assists,

			LP: match.Subject.Lp.LpDiff,
		}

		championID := match.Subject.ChampionId
		champ, ok := c.champs[championID]
		if ok {
			m.Champion = champ
		}

		matches = append(matches, m)
	}

	return matches, nil
}

const staticURL = `https://app.mobalytics.gg/api/league/gql/static/v1`
const queryChampions = `query LolCommonDataQuery {  
	champions: queryChampionsV1Contents(top: 200) {
	  flatData {
		riotId
		name
		__typename
	  }
	  __typename
	}
  }
`

func (c *Client) Champions(ctx context.Context) ([]leaguewatcher.Champion, error) {

	type Request struct {
		Query string `json:"query"`
	}

	req := Request{
		Query: queryChampions,
	}

	buf := bytes.NewBuffer(nil)
	err := json.NewEncoder(buf).Encode(req)
	if err != nil {
		return nil, err
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, staticURL, buf)
	if err != nil {
		return nil, err
	}
	r.Header.Add("Content-Type", "application/json")

	resp, err := c.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	type Response struct {
		Data struct {
			Champions []struct {
				FlatData struct {
					Name   string `json:"name"`
					RiotId int    `json:"riotId"`
				} `json:"flatData"`
			} `json:"champions"`
		} `json:"data"`
	}

	var respData Response
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		return nil, err
	}

	var champions []leaguewatcher.Champion
	for _, c := range respData.Data.Champions {
		champions = append(champions, leaguewatcher.Champion{
			ID:   c.FlatData.RiotId,
			Name: c.FlatData.Name,
		})
	}

	return champions, nil
}

func (c *Client) Sync(ctx context.Context) error {
	champs, err := c.Champions(ctx)
	if err != nil {
		return fmt.Errorf("get champs: %w", err)
	}

	for i, champ := range champs {
		c.champs[champ.ID] = champs[i]
	}

	return nil
}

const wsRefresh = `subscription LolSummonerUpdateSubscription($region: Region!, $gameName: String!, $tagLine: String!, $source: LoLProfileSource!) {
	lolProfileChanged(
	  input: {region: $region, gameName: $gameName, tagLine: $tagLine, source: $source}
	) {
	  ...LolSummonerUpdateFragment
	  __typename
	}
  }
  
  fragment LolSummonerUpdateFragment on LolProfileChangedPayload {
	event
	region
	gameName
	tagLine
	current
	total
	__typename
  }`

func (c *Client) RefreshProfile(ctx context.Context, region, summoner, tag string) (res string, err error) {
	ws := graphql.NewSubscriptionClient(mobaWS).
		WithTimeout(30 * time.Second).
		WithConnectionParams(map[string]interface{}{
			"Accept-Language": "en_us",
			"x-moba-client":   "mobalytics-web",
		}).
		WithProtocol(graphql.GraphQLWS)
		// WithLog(log.Println),
	defer func() {
		if err := ws.Close(); err != nil {
			c.logger.Error("close ws", zap.Error(err))
		} else {
			c.logger.Debug("ws closed")
		}
	}()

	vars := map[string]interface{}{
		"region":   strings.ToUpper(region),
		"gameName": strings.ToLower(summoner),
		"tagLine":  strings.ToLower(tag),
		"source":   "WEB",
	}

	_, err = ws.Exec(wsRefresh, vars, func(message []byte, err error) error {
		if err != nil {
			return fmt.Errorf("onError: %w", err)
		}

		var resp struct {
			LolProfileChanged struct {
				Event string
			}
		}
		if err := json.Unmarshal(message, &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}

		res = resp.LolProfileChanged.Event
		return nil
	})

	if err != nil {
		return "", err
	}

	if err = ws.Run(); err != nil {
		return "", err
	}

	return res, nil
}
