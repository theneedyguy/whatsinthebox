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
	database Database
	client   *sql.DB
	secure   bool
)

const (
	itemsPerPage = 5
	version      = "v0.5.2"
	qrCodeSize   = 156
)

// Initialize Database and return sql connection to client
// Additionally also determine whether QR codes should use http:// or https:// as the schema via ENV
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

// Get all boxes and return html page and paginate them to only show a certain amount per page
func getBox(c *gin.Context) {
	// Page has always a default value if not provided by the request
	pageStr := c.DefaultQuery("page", "1")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	// The offset is always page -1 times the max items per page
	// Page 1 = 0 * totalItems
	// This means that the offset of the underlying SQL query will be 0 and the limit will be 'itemsPerPage'
	// Each page will only show 'itemsPerPage' with a certain offset to make pagination work
	offset := (page - 1) * itemsPerPage
	// We query the database for the total amount of boxes.
	// Will be used to calculate the amount of total pages displayed in the frontend.
	totalItems, err := database.GetBoxesTotal(client)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not get total boxes"})
		return
	}
	// Get boxes considering offsets and limits
	boxes, err := database.GetBoxesPaginated(client, offset, itemsPerPage)
	// Calculate the total amount of pages to display for the user in the frontend.
	// Right now this will be able to indefinitely "grow" in the user interface since we don't do any kind of "1,2,3,...,45" display in the frontend
	totalPages := int(math.Ceil(float64(totalItems) / float64(itemsPerPage)))
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not get boxes"})
		return
	}
	// Render the HTML page providing all values to it
	c.HTML(http.StatusOK, "boxes.tmpl", gin.H{
		"boxes":       boxes,
		"version":     version,
		"CurrentPage": page,
		"TotalPages":  totalPages,
	})
}

// Get all box contents for a certain box and return html page
func getBoxContent(c *gin.Context) {
	// Get the ID for the request and parse it to int
	idParam := c.Params.ByName("id")
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID for getting box content"})
		return
	}
	// Get all contents of the box
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
	// Define png byte slice to store qr code in
	var png []byte
	// Gets hostname and path from the request.
	currentURL := c.Request.Host + c.Request.RequestURI
	// Set schema to http(s) according to the environment variable HTTP_SECURE_SCHEMA
	schema := "http://"
	if secure {
		schema = "https://"
	}
	fullURL := schema + currentURL
	// Generate QR code with a defined size
	png, err = qrcode.Encode(fullURL, qrcode.Medium, qrCodeSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate QR code"})
		return
	}
	// Encode the qr code data as base64 and enclose it in a html image tag
	qrCodeBase64 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	qrCodeSafeURL := template.HTML(`<img src="` + qrCodeBase64 + `" alt="QR Code" />`)
	// Render the html page with the provided variables
	c.HTML(http.StatusOK, "content.tmpl", gin.H{
		"QRCode":   qrCodeSafeURL,
		"contents": contents,
	})
}

// Updates a boxes contents (Edit an item)
// Takes boxid and id as html parameters
func updateBoxContent(c *gin.Context) {
	boxidParam := c.Params.ByName("boxid")
	// BoxId will be used to redirect the user back to the correct page where they made the request from.
	boxid, err := strconv.Atoi(boxidParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Box ID"})
		return
	}
	idParam := c.Params.ByName("id")
	// Id will be used to identify which box to update
	id, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID for box content"})
		return
	}
	// Parse form data received from request.
	// item_name and item_amount from the form represent the new values for the item.
	c.Request.ParseForm()
	name := c.PostForm("item_name")
	quantityString := c.PostForm("item_amount")

	quantity, err := strconv.Atoi(quantityString)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Quantity"})
		return
	}
	// Update the box content with the provided values
	err = database.UpdateBoxContent(client, id, name, quantity)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not get boxes contents"})
		return
	}
	// Send user back to page where the request came from.
	c.Redirect(http.StatusFound, fmt.Sprintf("/box/%d", boxid))
}

// Creates a new box with the values parsed from the request form.
// Redirects the user back to the originating html page taking the page number into consideration
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
		c.Redirect(http.StatusFound, "/")
	} else {
		c.Redirect(http.StatusFound, fmt.Sprintf("/?page=%s", query))
	}

}

// Deletes a box from the database
// This request takes a JSON payload as the input, parses the value and uses it to execute the delete query in the database.
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
	// Deletes the box and all associated contents
	err = database.DeleteBox(client, boxid)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusInternalServerError, gin.H{"fail": "could not delete box"})
		return
	}
	// Respond to request with a json body and 200 status code
	c.JSON(http.StatusOK, gin.H{
		"message": "box deleted",
		"id":      req.ID,
	})
}

// Updates a boxes attributes
// Takes the boxid from the http params to identify the box
// Uses the form data to update attributes of the box
// Returns the user back to the website root
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

// Creates a new item in the specified box
// Uses the boxid to place the item into the correct box
// Takes the form data to set the attributes for the item and creates it
// Redirects the user to the box where the new content will be visible
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

// Deletes a single item from the database
// Takes a JSON payload, parses the id and uses the id to delete an item.
// Returns a JSON message and status code
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

// API endpoint to search boxes based on name or label name
// Method: GET
// URL: /api/v0/box
// Query Param: search
// Example: curl http://localhost/api/v0/box?search=box
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

// API endpoint to move one item to another box
// Method: PATCH
// URL: /api/v0/item/move
// Body: { "targetBox": 1, "sourceBox": 2, "sourceItem": 10 }
// Example: curl -XPATCH http://localhost/api/v0/item/move -d '{ "targetBox": 1, "sourceBox": 2, "sourceItem": 10 }'
func apiMoveItem(c *gin.Context) {
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
	// Initialize gin
	router := gin.Default()
	// Register helper functions for template rendering
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
	// Get all html templates from directory
	router.LoadHTMLGlob("templates/*")
	// Endpoint for web root
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

	// Group all API endpoints together
	apiV0 := router.Group("/api/v0")
	apiV0.GET("/box", apiGetBox)
	apiV0.PATCH("/item/move", apiMoveItem)

	// Run the website and bind to port provided from env variable PORT with default 8088
	router.Run(fmt.Sprintf("0.0.0.0:%s", getEnv("PORT", "8088")))
}
