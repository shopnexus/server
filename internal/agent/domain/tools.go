package domain

// ToolTitles are the MCP tools' titles, which the agent shows as a tool
// call's label and the settings show beside each tool's name.
var ToolTitles = map[string]string{
	"count":               "Counts",
	"lookup":              "What an id is",
	"ticket_get":          "Ticket",
	"ticket_messages":     "Ticket messages",
	"tickets_list":        "Support tickets",
	"tickets_of_account":  "Tickets of an account",
	"account_get":         "Account",
	"accounts_list":       "Accounts",
	"orders_of_account":   "Orders of an account",
	"listings_of_account": "Listings of an account",
	"console_actions":     "Console actions",
	"order_get":           "Order",
	"listing_get":         "Listing",
	"price_guide":         "Price guide",
	"report_cases_list":   "Report cases",
	"report_case_get":     "Report case",
}
