package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/auth0/go-auth0/management"
	"github.com/gorilla/websocket"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/auth0/auth0-cli/internal/ansi"
	"github.com/auth0/auth0-cli/internal/auth0"
	"github.com/auth0/auth0-cli/internal/display"
)

func universalLoginCustomizeBranding(cli *cli) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "customize",
		Args:  cobra.NoArgs,
		Short: "Customize the entire Universal Login Experience",
		Long:  "Customize and preview changes to the Universal Login Experience.",
		Example: `  auth0 universal-login customize
  auth0 ul customize`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			var dataToSend *pageData
			if err := ansi.Spinner("Gathering branding data. This will take a while", func() (err error) {
				dataToSend, err = fetchPageData(ctx, cli.api, cli.tenant)
				return err
			}); err != nil {
				return err
			}

			fmt.Fprintf(cli.renderer.MessageWriter, "Perform your changes within the UI"+"\n")

			err := startWebSocketServer(ctx, cli.renderer, cli.api, dataToSend)
			if err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

func startWebSocketServer(ctx context.Context, renderer *display.Renderer, api *auth0.API, pageData *pageData) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	handler := &webSocketHandler{
		renderer: renderer,
		api:      api,
		cancel:   cancel,
		sentData: pageData,
		port:     port,
	}

	server := &http.Server{
		Handler:      handler,
		ReadTimeout:  time.Minute * 10,
		WriteTimeout: time.Minute * 10,
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Serve(listener)
	}()

	if err := browser.OpenURL(fmt.Sprintf("http://localhost:5173?ws_port=%d", port)); err != nil {
		return err
	}

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return server.Close()
	}
}

type pageData struct {
	Connected             bool                               `json:"connected"`
	AuthenticationProfile *management.Prompt                 `json:"authentication_profile"`
	Branding              *management.Branding               `json:"branding"`
	Templates             *management.BrandingUniversalLogin `json:"templates"`
	Themes                *management.BrandingTheme          `json:"themes"`
	Tenant                *tenantData                        `json:"tenant"`
	CustomText            map[string]interface{}             `json:"custom_text"`
}

type tenantData struct {
	FriendlyName   string   `json:"friendly_name"`
	EnabledLocales []string `json:"enabled_locales"`
	Domain         string   `json:"domain"`
}

func fetchPageData(ctx context.Context, api *auth0.API, tenantDomain string) (*pageData, error) {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		return ensureCustomDomainIsEnabled(ctx, api)
	})

	var authenticationProfile *management.Prompt
	group.Go(func() (err error) {
		authenticationProfile, err = api.Prompt.Read()
		return err
	})

	var brandingSettings *management.Branding
	group.Go(func() (err error) {
		brandingSettings = fetchBrandingSettingsOrUseDefaults(ctx, api)
		return nil
	})

	var currentTemplate *management.BrandingUniversalLogin
	group.Go(func() (err error) {
		currentTemplate = fetchBrandingTemplateOrUseEmpty(ctx, api)
		return nil
	})

	var currentTheme *management.BrandingTheme
	group.Go(func() (err error) {
		currentTheme = fetchBrandingThemeOrUseEmpty(ctx, api)
		return nil
	})

	var tenant *management.Tenant
	group.Go(func() (err error) {
		tenant, err = api.Tenant.Read(management.Context(ctx))
		return err
	})

	var customText map[string]interface{}
	group.Go(func() (err error) {
		customText, err = fetchCustomTextWithDefaults(ctx, api)
		return err
	})

	if err := group.Wait(); err != nil {
		return nil, err
	}

	data := &pageData{
		Connected:             true,
		AuthenticationProfile: authenticationProfile,
		Branding:              brandingSettings,
		Templates:             currentTemplate,
		Themes:                currentTheme,
		Tenant: &tenantData{
			FriendlyName:   tenant.GetFriendlyName(),
			EnabledLocales: tenant.GetEnabledLocales(),
			Domain:         tenantDomain,
		},
		CustomText: customText,
	}

	return data, nil
}

func fetchBrandingThemeOrUseEmpty(ctx context.Context, api *auth0.API) *management.BrandingTheme {
	currentTheme, err := api.BrandingTheme.Default(management.Context(ctx))
	if err != nil {
		currentTheme = &management.BrandingTheme{
			Borders: management.BrandingThemeBorders{
				ButtonBorderRadius: 3,
				ButtonBorderWeight: 1,
				ButtonsStyle:       "rounded",
				InputBorderRadius:  3,
				InputBorderWeight:  1,
				InputsStyle:        "rounded",
				ShowWidgetShadow:   true,
				WidgetBorderWeight: 0,
				WidgetCornerRadius: 5,
			},
			Colors: management.BrandingThemeColors{
				BaseFocusColor:          auth0.String("#635dff"),
				BaseHoverColor:          auth0.String("#000000"),
				BodyText:                "#1e212a",
				Error:                   "#d03c38",
				Header:                  "#1e212a",
				Icons:                   "#65676e",
				InputBackground:         "#ffffff",
				InputBorder:             "#c9cace",
				InputFilledText:         "#000000",
				InputLabelsPlaceholders: "#65676e",
				LinksFocusedComponents:  "#635dff",
				PrimaryButton:           "#635dff",
				PrimaryButtonLabel:      "#ffffff",
				SecondaryButtonBorder:   "#c9cace",
				SecondaryButtonLabel:    "#1e212a",
				Success:                 "#13a688",
				WidgetBackground:        "#ffffff",
				WidgetBorder:            "#c9cace",
			},
			Fonts: management.BrandingThemeFonts{
				BodyText: management.BrandingThemeText{
					Bold: false,
					Size: 87.5,
				},
				ButtonsText: management.BrandingThemeText{
					Bold: false,
					Size: 100.0,
				},
				FontURL: "",
				InputLabels: management.BrandingThemeText{
					Bold: false,
					Size: 100.0,
				},
				Links: management.BrandingThemeText{
					Bold: true,
					Size: 87.5,
				},
				LinksStyle:        "normal",
				ReferenceTextSize: 16.0,
				Subtitle: management.BrandingThemeText{
					Bold: false,
					Size: 87.5,
				},
				Title: management.BrandingThemeText{
					Bold: false,
					Size: 150.0,
				},
			},
			PageBackground: management.BrandingThemePageBackground{
				BackgroundColor:    "#000000",
				BackgroundImageURL: "",
				PageLayout:         "center",
			},
			Widget: management.BrandingThemeWidget{
				HeaderTextAlignment: "center",
				LogoHeight:          52.0,
				LogoPosition:        "center",
				LogoURL:             "",
				SocialButtonsLayout: "bottom",
			},
		}
	}

	return currentTheme
}

func fetchCustomTextWithDefaults(ctx context.Context, api *auth0.API) (map[string]interface{}, error) {
	var availablePrompts = []string{
		"login",
		//"signup", "logout",
		//"consent", "device-flow", "email-otp-challenge", "email-verification", "invitation", "common",
		//"login-id", "login-password", "login-passwordless", "login-email-verification", "mfa", "mfa-email",
		//"mfa-otp", "mfa-phone", "mfa-push", "mfa-recovery-code", "mfa-sms", "mfa-voice", "mfa-webauthn",
		//"organizations", "reset-password", "signup-id", "signup-password", "status",
	}

	const language = "en"

	customText := make(map[string]interface{}, 0)
	for _, availablePrompt := range availablePrompts {
		promptText, err := api.Prompt.CustomText(availablePrompt, language)
		if err != nil {
			return nil, err
		}

		customText[availablePrompt] = promptText
	}

	request, err := api.HTTPClient.NewRequest(
		http.MethodGet,
		fmt.Sprintf("https://cdn.auth0.com/ulp/react-components/development/languages/%s/prompts.json", language),
		nil,
	)
	if err != nil {
		return customText, err
	}

	response, err := api.HTTPClient.Do(request)
	if err != nil {
		return customText, err
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return customText, err
	}

	defaultAllPromptsText := make([]map[string]interface{}, 0)
	if err := json.NewDecoder(response.Body).Decode(&defaultAllPromptsText); err != nil {
		return customText, err
	}

	defaultText := make(map[string]interface{}, 0)
	for _, value := range defaultAllPromptsText {
		for key, innerValue := range value {
			if key == "login" {
				defaultText[key] = innerValue
				break
			}

			//if key == "passkeys" {
			//	continue
			//}
			//innerInnerValue, ok := innerValue.(map[string]interface{})
			//if ok {
			//	for k := range innerInnerValue {
			//		if key == "reset-password" {
			//			if k == "reset-password-mfa-email-challenge" ||
			//				k == "reset-password-mfa-otp-challenge" ||
			//				k == "reset-password-mfa-phone-challenge" ||
			//				k == "reset-password-mfa-push-challenge-push" ||
			//				k == "reset-password-mfa-recovery-code-challenge" ||
			//				k == "reset-password-mfa-sms-challenge" ||
			//				k == "reset-password-mfa-voice-challenge" ||
			//				k == "reset-password-mfa-webauthn-platform-challenge" ||
			//				k == "reset-password-mfa-webauthn-roaming-challenge" {
			//				delete(innerInnerValue, k)
			//			}
			//		}
			//	}
			//}

			//defaultText[key] = innerInnerValue
		}
	}

	return mergeMaps(defaultText, customText), nil
}

func mergeMaps(map1, map2 map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for key, value := range map1 {
		if subMap, ok := value.(map[string]interface{}); ok {
			if subMap2, ok := map2[key].(map[string]interface{}); ok {
				merged[key] = mergeMaps(subMap, subMap2)
				if len(merged[key].(map[string]interface{})) == 0 {
					delete(merged, key)
				}
			} else {
				merged[key] = subMap
				if len(merged[key].(map[string]interface{})) == 0 {
					delete(merged, key)
				}
			}
		} else {
			if map2Value, ok := map2[key]; ok {
				merged[key] = map2Value
			} else {
				merged[key] = value
			}
		}
	}
	for key, value := range map2 {
		if _, ok := merged[key]; !ok {
			merged[key] = value
		}
	}
	return merged
}

type webSocketHandler struct {
	renderer     *display.Renderer
	api          *auth0.API
	receivedData *pageData
	sentData     *pageData
	cancel       context.CancelFunc
	port         int
}

func (h *webSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header["Origin"]
			if len(origin) == 0 {
				return true
			}
			u, err := url.Parse(origin[0])
			if err != nil {
				return false
			}

			return u.String() == "http://localhost:5173"
		},
	}

	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("error accepting WebSocket connection: %v", err)
		return
	}

	connection.SetReadLimit(1e+6) // 1 MB.

	if err = connection.WriteJSON(&h.sentData); err != nil {
		log.Printf("failed to write message: %v", err)
		h.cancel()
		return
	}

	for {
		var msg pageData
		if err := connection.ReadJSON(&msg); err != nil {
			log.Printf("error reading from WebSocket: %v", err)
			h.cancel()
			return
		}

		h.receivedData = &msg

		if !h.receivedData.Connected {
			if err = connection.Close(); err != nil {
				log.Printf("error closing WebSocket: %v", err)
				h.cancel()
			}

			fmt.Fprintf(h.renderer.MessageWriter, "Disconnected from the UI. Test the Universal Login by running: 'auth0 test login'"+"\n")

			h.cancel()
		}

		if err := ansi.Spinner("Persisting branding data. This will take a while", func() error {
			return persistData(r.Context(), h.api, h.receivedData)
		}); err != nil {
			log.Printf("error persisting data: %+v", err)
			h.cancel()
			return
		}

		fmt.Fprintf(h.renderer.MessageWriter, "Branding for the Universal Login updated ✓"+"\n")
	}
}

func persistData(ctx context.Context, api *auth0.API, data *pageData) error {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() (err error) {
		return api.Branding.SetUniversalLogin(
			&management.BrandingUniversalLogin{
				Body: data.Templates.Body,
			},
		)
	})

	group.Go(func() (err error) {
		data.Themes.ID = nil

		existingTheme, err := api.BrandingTheme.Default()
		if err != nil {
			return api.BrandingTheme.Create(data.Themes)
		}

		return api.BrandingTheme.Update(existingTheme.GetID(), data.Themes)
	})

	group.Go(func() (err error) {
		return api.Prompt.Update(data.AuthenticationProfile)
	})

	group.Go(func() (err error) {
		data.Branding.LogoURL = nil
		return api.Branding.Update(data.Branding)
	})

	for key, value := range data.CustomText {
		key := key
		value := value
		group.Go(func() (err error) {
			bytes, err := json.Marshal(&value)
			if err != nil {
				return err
			}

			if strings.Contains(string(bytes), "{}") {
				return nil
			}

			data := make(map[string]interface{})
			err = json.Unmarshal(bytes, &data)
			if err != nil {
				return err
			}

			if len(data) == 0 {
				return nil
			}

			return api.Prompt.SetCustomText(key, "en", data)
		})
	}

	return group.Wait()
}