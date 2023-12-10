package main

type PackageDetails struct {
	Sno               string           `json:"sno"`
	TrackingStatus    string           `json:"tracking_status"`
	EstimatedDelivery string           `json:"estimated_delivery"`
	Details           []TrackingDetail `json:"details"`
	Recipient         RecipientInfo    `json:"recipient"`
	CurrentLocation   LocationInfo     `json:"current_location"`
}

type TrackingDetail struct {
	ID            int    `json:"id"`
	Date          string `json:"date"`
	Time          string `json:"time"`
	Status        string `json:"status"`
	LocationID    int    `json:"location_id"`
	LocationTitle string `json:"location_title"`
}

type RecipientInfo struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

type LocationInfo struct {
	LocationID int    `json:"location_id"`
	Title      string `json:"title"`
	City       string `json:"city"`
	Address    string `json:"address"`
}
