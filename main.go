package main

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"

	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
	"github.com/skip2/go-qrcode"
)

var (
	version  = "v0.5"
	database Database
	client   *sql.DB
	secure   bool
)

// Initialize Database and return sql connection to client
func init() {
	database = Database{
		DBFilePath: getEnv("DB", "/tmp/boxes.db"),
	}
	client = database.Init()
	var err error
	secure, err = strconv.ParseBool(getEnv("HTTP_SECURE_SCHEMA", "0"))
	if err != nil {
		secure = false
	}
}

// Template function to pretty print time data types as string
func formatAsDate(t time.Time) string {
	year, month, day := t.Date()
	return fmt.Sprintf("%d/%02d/%02d", year, month, day)
}

// Handle setting of variables of env var is not set
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return defaultValue
	}
	return value
}

// Get all boxes and return html page
func getBox(c *gin.Context) {

	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	// Define items per page
	const itemsPerPage = 5
	offset := (page - 1) * itemsPerPage
	totalItems, err := database.GetBoxesTotal(client)
	boxes, err := database.GetBoxesPaginated(client, offset, itemsPerPage)
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))

	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not get boxes"})
		return
	}
	c.HTML(http.StatusOK, "boxes.tmpl", gin.H{
		"boxes":       boxes,
		"version":     version,
		"CurrentPage": page,
		"TotalPages":  totalPages,
	})
}

// Get all box contents for a certain box and return html page
func getBoxContent(c *gin.Context) {
	idParam := c.Params.ByName("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID for getting box content"})
		return
	}
	contents, err := database.GetBoxContent(client, id)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not get box contents"})
		return
	}

	// User opened an invalid box.
	if len(contents) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"fail": "box does not exist"})
		return
	}

	var png []byte
	currentURL := c.Request.Host + c.Request.RequestURI
	schema := "http://"
	if secure {
		schema = "https://"
	}
	log.Println(schema)
	log.Println(currentURL)
	fullURL := schema + currentURL                        // Or "https://" if using HTTPS
	png, err = qrcode.Encode(fullURL, qrcode.Medium, 156) // 256x256 image
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate QR code"})
		return
	}
	qrCodeBase64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	qrCodeSafeURL := template.HTML(`<img src="` + qrCodeBase64 + `" alt="QR Code" />`)
	c.HTML(http.StatusOK, "content.tmpl", gin.H{
		"QRCode":   qrCodeSafeURL,
		"contents": contents,
	})
}

func updateBoxContent(c *gin.Context) {
	boxidParam := c.Params.ByName("boxid")
	boxid, err := strconv.Atoi(boxidParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Box ID"})
		return
	}

	idParam := c.Params.ByName("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID for box content"})
		return
	}
	c.Request.ParseForm()
	name := c.PostForm("item_name")
	quantityString := c.PostForm("item_amount")

	quantity, err := strconv.Atoi(quantityString)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Quantity"})
		return
	}

	err = database.UpdateBoxContent(client, id, name, quantity)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not get boxes contents"})
		return
	}
	c.Redirect(http.StatusFound, fmt.Sprintf("/box/%d", boxid))
}

func createBox(c *gin.Context) {
	c.Request.ParseForm()
	name := c.PostForm("item_name")
	label := c.PostForm("item_label")
	err := database.CreateBox(client, name, label)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not create new box"})
		return
	}
	query, exists := c.GetQuery("page")
	if !exists {
		c.Redirect(http.StatusFound, fmt.Sprintf("/"))
	} else {
		c.Redirect(http.StatusFound, fmt.Sprintf("/?page=%s", query))
	}

}

func deleteBox(c *gin.Context) {
	type DeleteRequest struct {
		ID string `json:"id" binding:"required"`
	}
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	boxid, err := strconv.Atoi(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Box ID"})
		return
	}
	err = database.DeleteBox(client, boxid)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not delete box"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "box deleted",
		"id":      req.ID,
	})
}

func updateBox(c *gin.Context) {
	idParam := c.Params.ByName("boxid")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID for box"})
		return
	}
	c.Request.ParseForm()
	name := c.PostForm("item_name")
	label := c.PostForm("item_label")

	err = database.UpdateBox(client, id, name, label)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not edit box"})
		return
	}
	c.Redirect(http.StatusFound, "/")
}

func createItem(c *gin.Context) {
	boxidParam := c.Params.ByName("boxid")
	boxid, err := strconv.Atoi(boxidParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Box ID for new item"})
		return
	}
	c.Request.ParseForm()
	name := c.PostForm("item_name")
	quantityString := c.PostForm("item_amount")
	quantity, err := strconv.Atoi(quantityString)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Quantity for item"})
		return
	}
	err = database.CreateItem(client, boxid, name, quantity)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not create new item in box"})
		return
	}
	c.Redirect(http.StatusFound, fmt.Sprintf("/box/%d", boxid))
}

func deleteItem(c *gin.Context) {
	type DeleteRequest struct {
		ID string `json:"id" binding:"required"`
	}
	var req DeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	itemId, err := strconv.Atoi(req.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Item ID"})
		return
	}
	err = database.DeleteItem(client, itemId)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not delete item"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "item deleted",
		"id":      req.ID,
	})
}

func apiGetBox(c *gin.Context) {
	query := c.DefaultQuery("search", "")
	boxes, err := database.GetBoxesByTextV0(client, query)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": "could not query boxes. Internal Server Error.",
		})
		return
	}
	if len(boxes) == 0 {
		boxes = make([]Box, 0)
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"count":   len(boxes),
		"result":  boxes,
	})

}

func moveItem(c *gin.Context) {
	type MoveRequest struct {
		TargetBox  int `json:"targetBox" binding:"required"`
		SourceBox  int `json:"sourceBox" binding:"required"`
		SourceItem int `json:"sourceItem" binding:"required"`
	}
	var req MoveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := database.MoveItem(client, req.SourceBox, req.TargetBox, req.SourceItem)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not move item"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":  "item moved",
		"id":       req.SourceItem,
		"oldBoxId": req.SourceBox,
		"newBoxId": req.TargetBox,
	})
}

func main() {

	router := gin.Default()
	// Register helper functions for pagination
	router.SetFuncMap(template.FuncMap{
		"formatAsDate": formatAsDate,
		"add":          func(a, b int) int { return a + b },
		"sub":          func(a, b int) int { return a - b },
		"seq": func(start int, end int) []int {
			s := make([]int, end-start+1)
			for i := range s {
				s[i] = start + i
			}
			return s
		},
	})

	router.LoadHTMLGlob("templates/*")
	router.GET("/", getBox)

	// Group all Box endpoints together
	box := router.Group("/box")
	box.DELETE("/delete", deleteBox)
	box.POST("/create", createBox)
	box.POST("/:boxid/edit/:id", updateBoxContent)
	box.POST("/:boxid/edit", updateBox)
	box.POST("/:boxid/create", createItem)
	box.GET("/:id", getBoxContent)

	router.DELETE("/item", deleteItem)

	apiV0 := router.Group("/api/v0")
	apiV0.GET("/box", apiGetBox)
	apiV0.PATCH("/item/move", moveItem)

	router.Run(fmt.Sprintf("0.0.0.0:%s", getEnv("PORT", "8088")))
}
