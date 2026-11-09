// Package docs holds MongoDB documents.
package docs

// Address is stored inside the documents that hold one.
type Address struct {
	Street string `bson:"street"`
	City   string `bson:"city"`
}

// Customer is a document of its own.
type Customer struct {
	ID   string `bson:"_id"`
	Name string `bson:"name"`
}

// Invoice holds a copy of its customer and the address it ships to.
type Invoice struct {
	ID       string    `bson:"_id"`
	Customer Customer  `bson:"customer"`
	ShipTo   Address   `bson:"ship_to"`
	Lines    []Address `bson:"lines"`
}
