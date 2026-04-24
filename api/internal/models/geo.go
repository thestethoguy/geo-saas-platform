package models

// State represents a row from the `states` table, shaped for API responses.
type State struct {
	ID      int    `json:"id"`
	LGDCode string `json:"lgd_code"`
	Name    string `json:"name"`
	NameHi  string `json:"name_hi,omitempty"`
}

// District represents a row from the `districts` table.
type District struct {
	ID        int    `json:"id"`
	LGDCode   string `json:"lgd_code"`
	StateID   int    `json:"state_id"`
	Name      string `json:"name"`
	NameHi    string `json:"name_hi,omitempty"`
}

// SubDistrict represents a row from the `sub_districts` table.
type SubDistrict struct {
	ID         int    `json:"id"`
	LGDCode    string `json:"lgd_code"`
	DistrictID int    `json:"district_id"`
	Name       string `json:"name"`
	NameHi     string `json:"name_hi,omitempty"`
}

// Village represents a row from the `villages` table.
type Village struct {
	ID            int      `json:"id"`
	LGDCode       string   `json:"lgd_code"`
	SubDistrictID int      `json:"sub_district_id"`
	Name          string   `json:"name"`
	NameHi        string   `json:"name_hi,omitempty"`
	CensusCode    string   `json:"census_code,omitempty"`
	Pincode       string   `json:"pincode,omitempty"`
	Latitude      *float64 `json:"latitude,omitempty"`
	Longitude     *float64 `json:"longitude,omitempty"`
	Population    *int     `json:"population,omitempty"`
}

// APIResponse is the standard envelope for all list endpoints.
type APIResponse struct {
	Count int         `json:"count"`
	Data  interface{} `json:"data"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}
