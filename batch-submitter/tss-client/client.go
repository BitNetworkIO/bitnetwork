package tss_client

import (
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/golang-jwt/jwt/v4"
	"github.com/mantlenetworkio/mantle/l2geth/common/hexutil"
	"github.com/mantlenetworkio/mantle/tss/common"
	"github.com/syndtr/goleveldb/leveldb/errors"
)

const (
	JwtSecretLength = 32
)

var errTssHTTPError = errors.New("tss http error")

type TssClient interface {
	GetSignStateBatch(BatchData common.SignStateRequest) ([]byte, error)
}

type Client struct {
	client *resty.Client
}

type TssResponse struct {
	Signature []byte `json:"signature"`
	RollBack  bool   `json:"roll_back"`
}

func NewClient(url string, jwtSecretStr string) (*Client, error) {
	client := resty.New()
	client.SetHostURL(url)
	if len(jwtSecretStr) != 0 {
		jwtSecret, err := hexutil.Decode(jwtSecretStr)
		if err != nil {
			return nil, fmt.Errorf("invalid jwt secret %s", err.Error())
		}
		if len(jwtSecret) != JwtSecretLength {
			return nil, fmt.Errorf("invalid jwt secret length, expected length %d, actual length %d",
				JwtSecretLength, len(jwtSecret))
		}
		client.SetAuthScheme("Bearer")
		client.OnBeforeRequest(func(c *resty.Client, r *resty.Request) error {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"iat": &jwt.NumericDate{Time: time.Now()},
			})
			s, err := token.SignedString(jwtSecret[:])
			if err != nil {
				return fmt.Errorf("failed to create JWT token: %w", err)
			}
			r.Token = s
			return nil
		})
	}
	client.OnAfterResponse(func(c *resty.Client, r *resty.Response) error {
		statusCode := r.StatusCode()
		if statusCode >= 400 {
			method := r.Request.Method
			url := r.Request.URL
			return fmt.Errorf("%d cannot %s %s: %w", statusCode, method, url, errTssHTTPError)
		}
		return nil
	})
	return &Client{
		client: client,
	}, nil
}

func (c *Client) GetSignStateBatch(BatchData common.SignStateRequest) ([]byte, error) {
	var signature []byte
	response, err := c.client.R().
		SetBody(map[string]interface{}{"start_block": BatchData.StartBlock, "offset_starts_at_index": BatchData.OffsetStartsAtIndex, "state_roots": BatchData.StateRoots}).
		SetResult(signature).
		Post("/api/v1/sign/state")
	if err != nil {
		return nil, fmt.Errorf("cannot get signature: %w", err)
	}
	if response.StatusCode() == 200 {
		return response.Body(), nil
	} else {
		return nil, errors.New("fetch tss manager signature fail")
	}
}
