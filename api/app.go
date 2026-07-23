package api

import (
	"crynux_as/api/llm"
	"crynux_as/api/tools"
	v1 "crynux_as/api/v1"
	responseV1 "crynux_as/api/v1/response"
	"crynux_as/config"
	"reflect"
	"regexp"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/loopfz/gadgeto/tonic"
	log "github.com/sirupsen/logrus"
	"github.com/wI2L/fizz"
	"github.com/wI2L/fizz/openapi"
)

func GetHttpApplication(appConfig *config.AppConfig) *gin.Engine {

	gin.SetMode(appConfig.Environment)

	engine := gin.New()
	engine.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowCredentials: true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"*"},
	}))
	engine.Use(AccessLogger(log.StandardLogger()), gin.Recovery())
	engine.Use(APIVersion())

	fizzEngine := fizz.NewFromEngine(engine)

	// Do not include package name in component names
	fizzEngine.Generator().UseFullSchemaNames(false)

	// Initialize our own handlers
	tonic.SetErrorHook(TonicResponseErrorHook)
	tonic.SetRenderHook(TonicRenderHook, "")
	tonic.SetBindHook(tonic.DefaultBindingHookMaxBodyBytes(appConfig.Http.MaxBodyBytes))
	tonic.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		return name
	})

	// Hello page
	fizzEngine.GET("", []fizz.OperationOption{}, Hello)

	tools.InitializeJWTManager()

	// v1 api
	v1.InitRoutes(fizzEngine)

	// Private OpenAI-compatible LLM API endpoints
	llm.InitRoutes(engine)

	// Serve OpenAPI specifications
	infos := &openapi.Info{
		Title:       "Crynux AS",
		Description: "The AI Services backend for the Crynux Network",
		Version:     "1.0.0",
	}

	fizzEngine.GET("/openapi.json", nil, fizzEngine.OpenAPI(infos, "json"))
	fizzEngine.GET("/openapi.yml", nil, fizzEngine.OpenAPI(infos, "yaml"))

	if len(fizzEngine.Errors()) != 0 {

		for _, err := range fizzEngine.Errors() {
			log.Error(err)
		}

		panic("fizz initialization error")
	}

	return engine
}

func APIVersion() gin.HandlerFunc {
	return func(c *gin.Context) {

		path := c.FullPath()

		re := regexp.MustCompile(`^/v([0-9]+)/`)
		matches := re.FindStringSubmatch(path)

		if len(matches) > 1 {
			c.Set("api_version", matches[1])
		}

		c.Next()
	}
}

// TonicResponseErrorHook distributes error handling to implementations in different API versions
func TonicResponseErrorHook(ctx *gin.Context, err error) (int, interface{}) {
	apiVersion := ctx.GetString("api_version")
	switch apiVersion {
	case "1":
		return responseV1.TonicErrorResponse(ctx, err)
	default:
		return tonic.DefaultErrorHook(ctx, err)
	}
}

func TonicRenderHook(ctx *gin.Context, statusCode int, payload interface{}) {
	apiVersion := ctx.GetString("api_version")
	switch apiVersion {
	case "1":
		responseV1.TonicRenderResponse(ctx, statusCode, payload)
	default:
		tonic.DefaultRenderHook(ctx, statusCode, payload)
	}
}
