package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"github.com/jung-kurt/gofpdf/v2"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

type PaymentForm struct {
	CardNumber     string `json:"cardNumber"`
	ExpirationDate string `json:"expirationDate"`
	CVV            string `json:"cvv"`
	Name           string `json:"name"`
	Address        string `json:"address"`
}

type Product struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Transaction struct {
	ID         string    `json:"id" bson:"_id,omitempty"`
	CartItems  []Product `json:"cartItems"`
	Customer   Customer  `json:"customer"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	TotalPrice float64   `json:"totalPrice"`
}

var client *mongo.Client
var transactionCollection *mongo.Collection

func main() {

	loadEnv()

	clientOptions := options.Client().ApplyURI(os.Getenv("MONGO_URI"))
	var err error
	client, err = mongo.Connect(context.TODO(), clientOptions)
	if err != nil {
		log.Fatal(err)
	}

	err = client.Ping(context.TODO(), readpref.Primary())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Connected to MongoDB!")

	transactionCollection = client.Database("marketplace").Collection("transactions")

	router := gin.Default()

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:3000"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	router.POST("/create-transaction", createTransaction)
	router.GET("/transactions/:customerId", getTransactions)
	router.POST("/process-payment", processPayment)
	router.GET("/pay/:TransactionId", pay)
	router.Run(":8081")
}

func loadEnv() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
}

func pay(c *gin.Context) {
	transactionID := c.Param("TransactionId")

	objectId, _ := primitive.ObjectIDFromHex(transactionID)

	filter := bson.M{"_id": objectId}

	update := bson.M{
		"$set": bson.M{
			"status":    "paid",
			"updatedAt": time.Now(),
		},
	}

	options := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var updatedTransaction Transaction
	err := transactionCollection.FindOneAndUpdate(context.Background(), filter, update, options).Decode(&updatedTransaction)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update transaction status"})
		return
	}

	c.JSON(http.StatusOK, updatedTransaction)
}

func processPayment(c *gin.Context) {
	var paymentForm PaymentForm
	if err := c.ShouldBindJSON(&paymentForm); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

func createTransaction(c *gin.Context) {
	var transaction Transaction
	if err := c.BindJSON(&transaction); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	transaction.TotalPrice = calculateTotalPrice(transaction.CartItems)
	transaction.Status = "awaiting payment"
	transaction.CreatedAt = time.Now()
	transaction.UpdatedAt = time.Now()

	_, err := transactionCollection.InsertOne(context.Background(), transaction)
	if err != nil {
		fmt.Print("failed to insert to DB", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	generatePDF(transaction.CartItems, transaction.TotalPrice, len(transaction.CartItems))

	c.JSON(http.StatusOK, gin.H{"transaction": transaction /*"receipt": receiptFilePath*/})
}

func getTransactions(c *gin.Context) {
	customerId := c.Param("customerId")
	var transactions []Transaction

	filter := bson.M{"customer.id": customerId}
	cursor, err := transactionCollection.Find(context.Background(), filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer cursor.Close(context.Background())

	for cursor.Next(context.Background()) {
		var transaction Transaction
		cursor.Decode(&transaction)
		transactions = append(transactions, transaction)
	}

	c.JSON(http.StatusOK, transactions)
}

func calculateTotalPrice(items []Product) float64 {
	var total float64
	for _, item := range items {
		total += item.Price
	}
	return total
}

type Item struct {
	name     string
	unitCost float64
	quantity float64
	total    float64
}

func generatePDF(items []Product, totalPrice float64, totalNumberOfItems int) {
	pdf := gofpdf.New("P", "mm", "A4", "")

	pdf.AddPage()

	pdf.SetFont("Arial", "B", 16)

	pdf.Cell(0, 10, "START OF FISCAL RECEIPT")
	pdf.Ln(12)

	pdf.SetFont("Arial", "", 12)

	pdf.Cell(0, 10, "Maximus & Kuka Ltd")
	pdf.Ln(8)
	pdf.Cell(0, 10, "TIN: 098908978")
	pdf.Ln(8)
	pdf.Cell(0, 10, "Welcome to our shop!")
	pdf.Ln(12)

	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())
	pdf.Ln(5)

	for _, item := range items {
		pdf.CellFormat(100, 10, item.Name, "0", 0, "L", false, 0, "")
		pdf.CellFormat(40, 10, fmt.Sprintf("%.2f x %.1f", item.Price), "0", 0, "L", false, 0, "")
		pdf.Ln(8)
	}

	pdf.Ln(5)

	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())

	pdf.Cell(0, 10, "NUMBER OF ITEMS")
	pdf.CellFormat(0, 10, fmt.Sprintf("%d", totalNumberOfItems), "0", 1, "R", false, 0, "")

	pdf.Cell(0, 10, "TOTAL")
	pdf.CellFormat(0, 10, fmt.Sprintf("%.2f", totalPrice), "0", 1, "R", false, 0, "")

	pdf.Cell(0, 10, "CARD")
	pdf.CellFormat(0, 10, fmt.Sprintf("%.2f", totalPrice), "0", 1, "R", false, 0, "")

	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())
	pdf.Ln(5)

	pdf.CellFormat(0, 10, "THANK YOU", "", 1, "C", false, 0, "")
	pdf.CellFormat(0, 10, "COME BACK AGAIN", "", 1, "C", false, 0, "")

	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())
	pdf.Ln(5)

	err := pdf.OutputFileAndClose("receipt.pdf")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Receipt generated successfully")
	}
}
