package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/julienschmidt/httprouter"
)

var tpl *template.Template
var db *sql.DB

func init() {
	tpl = template.Must(template.ParseGlob("templates/*"))
}

type Server struct {
	r *httprouter.Router
}

type Reservation struct {
	reserveNum int
	startDate  time.Time
	endDate    time.Time
	charge     float32
	roomNum    int
	guestID    int
}

type Guest struct {
	guestID           int
	lastName          string
	firstName         string
	paymentCardNumber string
	phoneNumber       string
	email             string
	billingAddr       string
}

type Room struct {
	roomNum int
	price   float32
}

func main() {
	fmt.Printf("Mia Resort App Running\n")
	var err error
	db, err = sql.Open("mysql", "root:"+os.Getenv("MIA_DB_PASS")+"@/mia_db?parseTime=true")

	if err != nil {
		fmt.Printf("There was an error connecting to the database.")
	}

	defer db.Close()

	err = db.Ping()

	if err != nil {
		panic(err.Error())
	}

	mux := httprouter.New()
	// The main menu
	mux.GET("/", index)
	// The page where it shows a list of rooms
	mux.GET("/reserve", reserve)
	// Page where user selects date and enters personal informationm
	mux.GET("/reserve/:roomType/:roomView/book", GetBookRoom)
	// POSTS reservation information
	mux.POST("/reserve", PostReservation)
	// "Reservation Successful"
	mux.GET("/status", status)
	// View the reservation
	mux.GET("/view", viewReservation)
	mux.POST("/reservation", reservation)
	mux.GET("/services", services)
	mux.GET("/services/add/:guestID", servicesAdd)
	mux.POST("/addService", PostService)
	mux.GET("/view_invoice", viewInvoice)
	mux.POST("/getInvoice", getInvoice)
	mux.GET("/invoice/:guestID", invoice)

	// Serves the css files called by HTML files
	mux.ServeFiles("/assets/css/*filepath", http.Dir("assets/css/"))

	log.Fatal(http.ListenAndServe(":8080", &Server{mux}))
}

// Sets up CORS for all requests
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers",
			"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials",
			"true")
	}
	// Stop here if its Preflighted OPTIONS request
	if r.Method == "OPTIONS" {
		errors.New("Request method is OPTIONS")
	}
	s.r.ServeHTTP(w, r)
}

// Serves the main menu page
func index(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	err := tpl.ExecuteTemplate(w, "index.gohtml", nil)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

type RoomCharge struct {
	Price        float32
	RoomTypeName string
	ViewName     string
}

// Serves the reserve reservation page with list of RoomCharges (types of Rooms)
func reserve(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	roomCharges := make([]RoomCharge, 0)

	rows, err := db.Query("SELECT * FROM RoomCharge;")
	defer rows.Close()

	for rows.Next() {
		roomCharge := RoomCharge{}
		scanErr := rows.Scan(&roomCharge.Price, &roomCharge.RoomTypeName, &roomCharge.ViewName)
		if scanErr != nil {
			fmt.Println("There was an error scanning the roomCharges")
		}
		roomCharges = append(roomCharges, roomCharge)
	}

	err = tpl.ExecuteTemplate(w, "reserve.gohtml", roomCharges)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

// Information to be passed to GetBookRoom Handler
type BookRoomInfo struct {
	RoomType string
	RoomView string
	Error    string
}

// Serves the page where a user enters dates and personal info
func GetBookRoom(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {

	// roomType needs to be included into the form value to be submitted as a reservation
	bookInfo := BookRoomInfo{}
	bookInfo.RoomType = ps.ByName("roomType")
	bookInfo.RoomView = ps.ByName("roomView")

	err := tpl.ExecuteTemplate(w, "bookroom.gohtml", bookInfo)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

type StatusInfo struct {
	GuestID    int
	ReserveNum int
}

func PostReservation(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	if req.Method != http.MethodPost {
		http.Error(w, http.StatusText(405), http.StatusMethodNotAllowed)
		return
	}
	// Get all the form values

	firstName := req.FormValue("firstName")
	lastName := req.FormValue("lastName")
	startDate := req.FormValue("startDate")
	endDate := req.FormValue("endDate")
	email := req.FormValue("email")
	paymentCardNum := req.FormValue("creditCard")
	phoneNum := req.FormValue("phoneNum")
	billingAddr := req.FormValue("billingAddr")
	roomType := req.FormValue("roomType")
	roomView := req.FormValue("roomView")

	guest := Guest{}
	guest.lastName = lastName
	guest.firstName = firstName
	guest.paymentCardNumber = paymentCardNum
	guest.phoneNumber = phoneNum
	guest.email = email
	guest.billingAddr = billingAddr

	err := db.QueryRow(`SELECT MAX(guestID) FROM Guest;`).Scan(&guest.guestID)

	guest.guestID += 1

	if err != nil {
		fmt.Println("There was an error scanning guestID to guest.guestID")
	}

	var price int

	err = db.QueryRow(`SELECT price FROM RoomCharge WHERE roomTypeName=? AND viewName=?;`,
		roomType, roomView).Scan(&price)

	var roomNum int
	err = db.QueryRow(`SELECT roomNum FROM Room WHERE roomNum 
		NOT IN (SELECT roomNum FROM Reservation) AND Room.price=?`, price).Scan(&roomNum)

	if err != nil {
		// Redirect back to /:roomType/:roomView/book with error notification
		// that there are no rooms available
		bookInfo := BookRoomInfo{}
		bookInfo.RoomType = roomType
		bookInfo.RoomView = roomView
		bookInfo.Error = "Room not available"
		w.WriteHeader(400)
		err = tpl.ExecuteTemplate(w, "bookroom.gohtml", bookInfo)
	}

	_, err = db.Exec(`INSERT INTO Guest (guestID, lastName, firstName, 
		paymentCardNumber, phoneNumber, email, billingAddr) 
		VALUES (?, ?, ?, ?, ?, ?, ?);`, guest.guestID, guest.lastName,
		guest.firstName, guest.paymentCardNumber, guest.phoneNumber,
		guest.email, guest.billingAddr)

	if err != nil {
		fmt.Println(err.Error())
	}

	startTime, err := time.Parse("2006-01-02", startDate)
	if err != nil {
		fmt.Println("Error parsing startDate")
	}
	endTime, err := time.Parse("2006-01-02", endDate)
	if err != nil {
		fmt.Println("Error parsing endDate")
	}

	var reserveNum int
	err = db.QueryRow(`SELECT MAX(reserveNum) FROM Reservation;`).Scan(&reserveNum)
	reserveNum += 1

	_, err = db.Exec(`INSERT INTO Reservation (reserveNum, startDate, endDate, 
		charge, roomNum, guestID) 
		VALUES (?, ?, ?, ?, ?, ?);`, reserveNum, startTime,
		endTime, price, roomNum, guest.guestID)
	if err != nil {
		fmt.Println("Error inserting Reservation into DB")
	}

	var invoiceNum int
	err = db.QueryRow(`SELECT MAX(invoiceNum) FROM Invoice;`).Scan(&invoiceNum)
	invoiceNum += 1

	_, err = db.Exec(`INSERT INTO Invoice (invoiceNum, guestID, reserveNum, totalCharge) 
		VALUES (?, ?, ?, ?);`, invoiceNum, guest.guestID, reserveNum, price)

	if err != nil {
		fmt.Println(err.Error())
	}

	statInfo := StatusInfo{}
	statInfo.GuestID = guest.guestID
	statInfo.ReserveNum = reserveNum

	// Redirect to /status with reservation information
	w.WriteHeader(200)
	err = tpl.ExecuteTemplate(w, "status.gohtml", statInfo)

	if err != nil {
		fmt.Println(err.Error())
	}
}

// Serves the reservation made status page
func status(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	err := tpl.ExecuteTemplate(w, "status.gohtml", nil)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

// Serves the view rooms page
func viewReservation(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	err := tpl.ExecuteTemplate(w, "view.gohtml", nil)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

type ReservationInfo struct {
	ReserveNum int
	StartDate  string
	EndDate    string
	Charge     float32
	RoomType   string
	ViewName   string
	GuestID    int
}

// Serves the reservation page
func reservation(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	// Given the first and last name, find ReservationInfo
	ReserveInfo := ReservationInfo{}
	firstName := req.FormValue("firstName")
	var guestID int
	err := db.QueryRow(`SELECT guestID from Guest WHERE firstName=?`,
		firstName).Scan(&guestID)

	var timeStart time.Time
	var timeEnd time.Time

	err = db.QueryRow(`SELECT reserveNum, startDate, endDate, charge  
		FROM Reservation WHERE guestID=?;`, guestID).
		Scan(&ReserveInfo.ReserveNum, &timeStart,
			&timeEnd, &ReserveInfo.Charge)

	if err != nil {
		fmt.Println(err.Error())
	}

	ReserveInfo.GuestID = guestID
	ReserveInfo.StartDate = timeStart.Format("2006-01-02")
	ReserveInfo.EndDate = timeEnd.Format("2006-01-02")

	// Need to find RoomType
	err = db.QueryRow(`SELECT roomTypeName, viewName FROM RoomCharge
		WHERE price=?`, ReserveInfo.Charge).Scan(&ReserveInfo.RoomType,
		&ReserveInfo.ViewName)

	if err != nil {
		fmt.Println(err.Error())
	}

	err = tpl.ExecuteTemplate(w, "reservation.gohtml", ReserveInfo)

	if err != nil {
		fmt.Println("Error executing template reservation.gohtml")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}
}

type Service struct {
	ServiceID int
	Name      string
	Price     int
	GuestID   string
}

// Serves the view all services page
func services(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	services := make([]Service, 0)
	rows, err := db.Query(`SELECT * FROM Service;`)
	defer rows.Close()

	for rows.Next() {
		service := Service{}
		err = rows.Scan(&service.ServiceID, &service.Name, &service.Price)
		if err != nil {
			fmt.Println("Error scanning Service.")
			fmt.Println(err.Error())
		}
		services = append(services, service)
	}

	err = tpl.ExecuteTemplate(w, "services.gohtml", services)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

// Serves the add service page
func servicesAdd(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	services := make([]Service, 0)
	rows, err := db.Query(`SELECT * FROM Service;`)
	defer rows.Close()

	for rows.Next() {
		service := Service{}
		err = rows.Scan(&service.ServiceID, &service.Name, &service.Price)
		if err != nil {
			fmt.Println("Error scanning Service.")
			fmt.Println(err.Error())
		}
		service.GuestID = ps.ByName("guestID")
		services = append(services, service)
	}

	err = tpl.ExecuteTemplate(w, "services_add.gohtml", services)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

// Handles /addService route
func PostService(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	guestID, err := strconv.Atoi(req.FormValue("guestID"))
	serviceID, err := strconv.Atoi(req.FormValue("serviceID"))
	charge, err := strconv.ParseFloat(req.FormValue("charge"), 32)
	dateUsed, err := time.Parse("2006-01-02", "2018-11-29")

	//fmt.Printf("Date service was purchased %s \n", dateUsed.String())

	_, err = db.Exec(`INSERT INTO ServiceUsed(guestID, serviceID, dateUsed, charge) 
		VALUES (?, ?, ?, ?)`, guestID, serviceID, dateUsed, charge)

	http.Redirect(w, req, "/invoice/"+strconv.Itoa(guestID), 301)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

type ServiceUsedInfo struct {
	ServiceID int
	Name      string
	Price     int
	DateUsed  string
}

type InvoiceInfo struct {
	ResInfo      ReservationInfo
	ServicesUsed []ServiceUsedInfo
}

// Enter name to see Invoice page
func viewInvoice(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {
	err := tpl.ExecuteTemplate(w, "view_invoice.gohtml", nil)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

// Enter name to see Invoice page
func getInvoice(w http.ResponseWriter, req *http.Request, _ httprouter.Params) {

	firstName := req.FormValue("firstName")
	lastName := req.FormValue("lastName")
	var guestID int
	err := db.QueryRow("SELECT getGuestID(?, ?)",
		firstName, lastName).Scan(&guestID)
	if guestID == -1 {
		// display "This guest does not exist" - something like that
		//
	} else {
		http.Redirect(w, req, "/invoice/"+strconv.Itoa(guestID), http.StatusSeeOther)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}

// Serves the invoice page
func invoice(w http.ResponseWriter, req *http.Request, ps httprouter.Params) {
	// Need to get reservations
	// Need to get ServiceUsed's
	ReserveInfo := ReservationInfo{}

	var timeStart time.Time
	var timeEnd time.Time
	guestID := ps.ByName("guestID")

	err := db.QueryRow(`SELECT reserveNum, startDate, endDate, charge  
		FROM Reservation WHERE guestID=?;`, guestID).
		Scan(&ReserveInfo.ReserveNum, &timeStart,
			&timeEnd, &ReserveInfo.Charge)

	if err != nil {
		fmt.Println(err.Error())
	}

	ReserveInfo.GuestID, err = strconv.Atoi(guestID)
	ReserveInfo.StartDate = timeStart.Format("2006-01-02")
	ReserveInfo.EndDate = timeEnd.Format("2006-01-02")

	// Need to find RoomType
	err = db.QueryRow(`SELECT roomTypeName, viewName FROM RoomCharge
		WHERE price=?`, ReserveInfo.Charge).Scan(&ReserveInfo.RoomType,
		&ReserveInfo.ViewName)

	if err != nil {
		fmt.Println(err.Error())
	}

	Invoice := InvoiceInfo{}
	Invoice.ResInfo = ReserveInfo
	Invoice.ServicesUsed = make([]ServiceUsedInfo, 0)
	rows, err := db.Query(`SELECT serviceID, charge FROM
	 ServiceUsed WHERE guestID=?;`, guestID)
	defer rows.Close()

	for rows.Next() {
		serviceUsed := ServiceUsedInfo{}
		err = rows.Scan(&serviceUsed.ServiceID, &serviceUsed.Price)
		if err != nil {
			fmt.Println("Error scanning ServiceUsedInfo.")
			fmt.Println(err.Error())
		}
		// Find Service Name
		err := db.QueryRow(`SELECT name FROM Service WHERE serviceID=?`,
			serviceUsed.ServiceID).Scan(&serviceUsed.Name)
		if err != nil {
			fmt.Println("Error scanning name from Service.")
			fmt.Println(err.Error())
		}
		Invoice.ServicesUsed = append(Invoice.ServicesUsed, serviceUsed)
	}

	err = tpl.ExecuteTemplate(w, "invoice.gohtml", Invoice)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		log.Fatalln(err)
	}

	return
}
