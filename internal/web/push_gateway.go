package web

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	defaultAPNSBaseURL        = "https://api.sandbox.push.apple.com"
	defaultAPNSProductionURL  = "https://api.push.apple.com"
	defaultFCMTokenURL        = "https://oauth2.googleapis.com/token"
	defaultFCMEndpointPattern = "https://fcm.googleapis.com/v1/projects/%s/messages:send"
)

type multiPushGateway struct {
	providers map[string]pushGateway
}

func (m *multiPushGateway) Send(ctx context.Context, reg store.PushRegistration, notification pushNotification) error {
	if m == nil {
		return nil
	}
	provider := m.providers[strings.ToLower(strings.TrimSpace(reg.Platform))]
	if provider == nil {
		return fmt.Errorf("unsupported push platform %q", reg.Platform)
	}
	return provider.Send(ctx, reg, notification)
}

func newEnvPushGateway() pushGateway {
	providers := map[string]pushGateway{}
	if apns, err := newAPNSGatewayFromEnv(); err != nil {
		log.Printf("push apns disabled: %v", err)
	} else if apns != nil {
		providers["apns"] = apns
	}
	if fcm, err := newFCMGatewayFromEnv(); err != nil {
		log.Printf("push fcm disabled: %v", err)
	} else if fcm != nil {
		providers["fcm"] = fcm
	}
	if len(providers) == 0 {
		return nil
	}
	return &multiPushGateway{providers: providers}
}

type apnsGateway struct {
	keyID      string
	teamID     string
	topic      string
	baseURL    string
	privateKey *ecdsa.PrivateKey
	client     *http.Client
	now        func() time.Time

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

func newAPNSGatewayFromEnv() (*apnsGateway, error) {
	keyID := strings.TrimSpace(os.Getenv("TABURA_PUSH_APNS_KEY_ID"))
	teamID := strings.TrimSpace(os.Getenv("TABURA_PUSH_APNS_TEAM_ID"))
	topic := strings.TrimSpace(os.Getenv("TABURA_PUSH_APNS_BUNDLE_ID"))
	if keyID == "" && teamID == "" && topic == "" {
		return nil, nil
	}
	if keyID == "" || teamID == "" || topic == "" {
		return nil, errors.New("TABURA_PUSH_APNS_KEY_ID, TABURA_PUSH_APNS_TEAM_ID, and TABURA_PUSH_APNS_BUNDLE_ID are required")
	}
	keyPEM := strings.TrimSpace(os.Getenv("TABURA_PUSH_APNS_PRIVATE_KEY"))
	if keyPEM == "" {
		path := strings.TrimSpace(os.Getenv("TABURA_PUSH_APNS_PRIVATE_KEY_PATH"))
		if path == "" {
			path = defaultSecretPath("apns-auth-key.p8")
		}
		keyPEM = readOptionalSecretFile(path)
	}
	keyPEM = strings.ReplaceAll(keyPEM, `\n`, "\n")
	if keyPEM == "" {
		return nil, errors.New("APNs private key is not configured")
	}
	privateKey, err := parseECPrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimSpace(os.Getenv("TABURA_PUSH_APNS_BASE_URL"))
	if baseURL == "" {
		if parseEnvBoolDefault("TABURA_PUSH_APNS_PRODUCTION", false) {
			baseURL = defaultAPNSProductionURL
		} else {
			baseURL = defaultAPNSBaseURL
		}
	}
	return &apnsGateway{
		keyID:      keyID,
		teamID:     teamID,
		topic:      topic,
		baseURL:    strings.TrimRight(baseURL, "/"),
		privateKey: privateKey,
		client:     &http.Client{Timeout: pushSendTimeout},
		now:        time.Now,
	}, nil
}

func (g *apnsGateway) Send(ctx context.Context, reg store.PushRegistration, notification pushNotification) error {
	if g == nil {
		return nil
	}
	token, err := g.authorizationToken()
	if err != nil {
		return err
	}
	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{
				"title": notification.Title,
				"body":  notification.Body,
			},
			"sound": "default",
		},
	}
	if len(notification.Data) > 0 {
		payload["tabura"] = notification.Data
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/3/device/"+url.PathEscape(strings.TrimSpace(reg.DeviceToken)), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", g.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("apns status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
}

func (g *apnsGateway) authorizationToken() (string, error) {
	if g == nil {
		return "", errors.New("apns gateway is nil")
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cachedToken != "" && now.Before(g.tokenExpiry) {
		return g.cachedToken, nil
	}
	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "ES256",
		"kid": g.keyID,
	})
	claimsJSON, _ := json.Marshal(map[string]any{
		"iss": g.teamID,
		"iat": now.Unix(),
	})
	unsigned := base64Raw(headerJSON) + "." + base64Raw(claimsJSON)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := ecdsa.SignASN1(rand.Reader, g.privateKey, digest[:])
	if err != nil {
		return "", err
	}
	g.cachedToken = unsigned + "." + base64Raw(signature)
	g.tokenExpiry = now.Add(50 * time.Minute)
	return g.cachedToken, nil
}

type fcmGateway struct {
	projectID   string
	tokenURL    string
	endpointURL string
	clientEmail string
	privateKey  *rsa.PrivateKey
	client      *http.Client
	now         func() time.Time

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func newFCMGatewayFromEnv() (*fcmGateway, error) {
	serviceAccount := strings.TrimSpace(os.Getenv("TABURA_PUSH_FCM_SERVICE_ACCOUNT_JSON"))
	if serviceAccount == "" {
		path := strings.TrimSpace(os.Getenv("TABURA_PUSH_FCM_SERVICE_ACCOUNT_PATH"))
		if path == "" {
			path = defaultSecretPath("fcm-service-account.json")
		}
		serviceAccount = readOptionalSecretFile(path)
	}
	if strings.TrimSpace(serviceAccount) == "" {
		return nil, nil
	}
	var creds struct {
		ProjectID   string `json:"project_id"`
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(serviceAccount), &creds); err != nil {
		return nil, fmt.Errorf("decode FCM service account JSON: %w", err)
	}
	privateKey, err := parseRSAPrivateKey(creds.PrivateKey)
	if err != nil {
		return nil, err
	}
	projectID := firstNonEmptyCalendarValue(strings.TrimSpace(os.Getenv("TABURA_PUSH_FCM_PROJECT_ID")), strings.TrimSpace(creds.ProjectID))
	if projectID == "" {
		return nil, errors.New("FCM project_id is required")
	}
	tokenURL := firstNonEmptyCalendarValue(strings.TrimSpace(os.Getenv("TABURA_PUSH_FCM_TOKEN_URL")), strings.TrimSpace(creds.TokenURI), defaultFCMTokenURL)
	endpointURL := strings.TrimSpace(os.Getenv("TABURA_PUSH_FCM_BASE_URL"))
	if endpointURL == "" {
		endpointURL = fmt.Sprintf(defaultFCMEndpointPattern, projectID)
	}
	return &fcmGateway{
		projectID:   projectID,
		tokenURL:    tokenURL,
		endpointURL: endpointURL,
		clientEmail: strings.TrimSpace(creds.ClientEmail),
		privateKey:  privateKey,
		client:      &http.Client{Timeout: pushSendTimeout},
		now:         time.Now,
	}, nil
}

func (g *fcmGateway) Send(ctx context.Context, reg store.PushRegistration, notification pushNotification) error {
	if g == nil {
		return nil
	}
	accessToken, err := g.oauthToken(ctx)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"message": map[string]any{
			"token": strings.TrimSpace(reg.DeviceToken),
			"notification": map[string]string{
				"title": notification.Title,
				"body":  notification.Body,
			},
		},
	}
	if len(notification.Data) > 0 {
		payload["message"].(map[string]any)["data"] = notification.Data
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpointURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("fcm status %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
}

func (g *fcmGateway) oauthToken(ctx context.Context) (string, error) {
	now := g.now()
	g.mu.Lock()
	if g.accessToken != "" && now.Before(g.tokenExpiry) {
		token := g.accessToken
		g.mu.Unlock()
		return token, nil
	}
	g.mu.Unlock()

	assertion, err := g.jwtAssertion(now)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("fcm token status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", err
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return "", errors.New("fcm token response missing access_token")
	}
	expiresIn := time.Duration(tokenResp.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 55 * time.Minute
	}
	g.mu.Lock()
	g.accessToken = tokenResp.AccessToken
	g.tokenExpiry = now.Add(expiresIn - time.Minute)
	g.mu.Unlock()
	return tokenResp.AccessToken, nil
}

func (g *fcmGateway) jwtAssertion(now time.Time) (string, error) {
	headerJSON, _ := json.Marshal(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	})
	claimsJSON, _ := json.Marshal(map[string]any{
		"iss":   g.clientEmail,
		"sub":   g.clientEmail,
		"aud":   g.tokenURL,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	unsigned := base64Raw(headerJSON) + "." + base64Raw(claimsJSON)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, g.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64Raw(signature), nil
}

func parseECPrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, errors.New("invalid APNs private key PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		parsed, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("APNs private key must be ECDSA")
		}
		return parsed, nil
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("failed to parse APNs private key")
}

func parseRSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(strings.TrimSpace(raw), `\n`, "\n")))
	if block == nil {
		return nil, errors.New("invalid FCM private key PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		parsed, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("FCM private key must be RSA")
		}
		return parsed, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, errors.New("failed to parse FCM private key")
}

func base64Raw(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

func generateTestECPrivateKeyPEM() (string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: encoded}
	return string(pem.EncodeToMemory(block)), nil
}
