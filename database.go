// PhishDetect
// Copyright (c) 2018-2020 Claudio Guarnieri.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Database struct {
	Client *mongo.Client
	DB     *mongo.Database
}

type User struct {
	Name      string    `json:"name" validate:"required"`
	Email     string    `json:"email" validate:"required,email"`
	Key       string    `json:"key"`
	Role      string    `json:"role"`
	Activated bool      `json:"activated"`
	Datetime  time.Time `json:"datetime"`
}

type Indicator struct {
	Type     string    `json:"type"`
	Original string    `json:"original"`
	Hashed   string    `json:"hashed"`
	Tags     []string  `json:"tags"`
	Datetime time.Time `json:"datetime"`
	Owner    string    `json:"owner"`
}

type Event struct {
	Type        string    `json:"type"`
	Match       string    `json:"match"`
	Indicator   string    `json:"indicator"`
	UserContact string    `json:"user_contact" bson:"user_contact"`
	Datetime    time.Time `json:"datetime"`
	UUID        string    `json:"uuid"`
	Key         string    `json:"key"`
}

type Report struct {
	Type        string    `json:"type"`
	Content     string    `json:"content"`
	UserContact string    `json:"user_contact" bson:"user_contact"`
	Datetime    time.Time `json:"datetime"`
	UUID        string    `json:"uuid"`
	Key         string    `json:"key"`
}

type Review struct {
	Indicator string    `json:"indicator"`
	Datetime  time.Time `json:"datetime"`
	Key       string    `json:"key"`
}

const IndicatorsLimitAll = 0
const IndicatorsLimit6Months = 1
const IndicatorsLimit24Hours = 2

func NewDatabase() (*Database, error) {
	client, err := mongo.NewClient(options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		return nil, err
	}
	err = client.Connect(context.TODO())
	if err != nil {
		return nil, err
	}
	db := client.Database("phishdetect")

	return &Database{
		Client: client,
		DB:     db,
	}, nil
}

func (d *Database) Close() {
	d.Client.Disconnect(context.Background())
}

func (d *Database) GetAllUsers() ([]User, error) {
	var users []User
	coll := d.DB.Collection("users")
	cur, err := coll.Find(context.Background(), bson.D{})
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.Background())

	for cur.Next(context.Background()) {
		var user User
		if err := cur.Decode(&user); err != nil {
			continue
		}
		users = append(users, user)
	}

	return users, nil
}

func (d *Database) ActivateUser(key string) error {
	coll := d.DB.Collection("users")

	_, err := coll.UpdateOne(context.Background(), bson.D{{"key", key}},
		bson.M{"$set": bson.M{"activated": true}})
	if err != nil {
		return err
	}

	return nil
}

func (d *Database) DeactivateUser(key string) error {
	coll := d.DB.Collection("users")

	_, err := coll.UpdateOne(context.Background(), bson.D{{"key", key}},
		bson.M{"$set": bson.M{"activated": false}})
	if err != nil {
		return err
	}

	return nil
}

func (d *Database) AddUser(user User) error {
	coll := d.DB.Collection("users")

	var userFound User
	err := coll.FindOne(context.Background(), bson.D{{"email", user.Email}}).Decode(&userFound)
	if err != nil {
		switch err {
		case mongo.ErrNoDocuments:
		default:
			return err
		}
	}

	_, err = coll.InsertOne(context.Background(), user)
	return err
}

func (d *Database) GetIndicators(limit int) ([]Indicator, error) {
	var iocs []Indicator
	coll := d.DB.Collection("indicators")

	now := time.Now().UTC()

	var filter bson.M

	switch limit {
	case IndicatorsLimitAll:
		filter = bson.M{}
	case IndicatorsLimit6Months:
		filter = bson.M{
			"datetime": bson.M{
				"$gte": now.AddDate(0, -6, 0),
			},
		}
	case IndicatorsLimit24Hours:
		filter = bson.M{
			"datetime": bson.M{
				"$gte": now.Add(-24 * time.Hour),
			},
		}
	}

	cur, err := coll.Find(context.Background(), filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.Background())

	for cur.Next(context.Background()) {
		var ioc Indicator
		if err := cur.Decode(&ioc); err != nil {
			continue
		}
		iocs = append(iocs, ioc)
	}

	return iocs, nil
}

func (d *Database) GetIndicatorByHash(hash string) (Indicator, error) {
	coll := d.DB.Collection("indicators")

	var ioc Indicator
	err := coll.FindOne(context.Background(), bson.D{{"hashed", hash}}).Decode(&ioc)
	if err != nil {
		return Indicator{}, err
	}

	return ioc, nil
}

func (d *Database) AddIndicator(ioc Indicator) error {
	coll := d.DB.Collection("indicators")

	var iocFound Indicator
	err := coll.FindOne(context.Background(), bson.D{{"hashed", ioc.Hashed}}).Decode(&iocFound)
	if err != nil {
		switch err {
		case mongo.ErrNoDocuments:
		default:
			return err
		}
	} else {
		// We update the data of the indicator, so that it get served again.
		_, err = coll.UpdateOne(context.Background(), bson.D{{"hashed", ioc.Hashed}},
			bson.M{"$set": bson.M{"datetime": time.Now().UTC()}})
		if err != nil {
			return err
		}
		return fmt.Errorf("This is an already known indicator")
	}

	_, err = coll.InsertOne(context.Background(), ioc)
	return err
}

func (d *Database) GetAllEvents(offset, limit int64) ([]Event, error) {
	coll := d.DB.Collection("events")

	opts := options.Find()
	opts.SetSort(bson.D{{"datetime", -1}})
	if offset > 0 {
		opts.SetSkip(offset)
	}
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cur, err := coll.Find(context.Background(), bson.D{}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.Background())

	events := []Event{}
	for cur.Next(context.Background()) {
		var event Event
		if err := cur.Decode(&event); err != nil {
			continue
		}
		events = append(events, event)
	}

	return events, nil
}

func (d *Database) AddEvent(event Event) error {
	coll := d.DB.Collection("events")

	_, err := coll.InsertOne(context.Background(), event)
	return err
}

func (d *Database) GetAllReports(offset, limit int64, reportType string) ([]Report, error) {
	coll := d.DB.Collection("reports")

	opts := options.Find()
	opts.SetSort(bson.D{{"datetime", -1}})
	if offset > 0 {
		opts.SetSkip(offset)
	}
	if limit > 0 {
		opts.SetLimit(limit)
	}

	filter := bson.D{}
	if reportType != "" {
		filter = bson.D{{"type", reportType}}
	}

	cur, err := coll.Find(context.Background(), filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(context.Background())

	reports := []Report{}
	for cur.Next(context.Background()) {
		var report Report
		if err := cur.Decode(&report); err != nil {
			continue
		}
		reports = append(reports, report)
	}

	return reports, nil
}

func (d *Database) AddReport(report Report) error {
	coll := d.DB.Collection("reports")

	_, err := coll.InsertOne(context.Background(), report)
	return err
}

func (d *Database) GetReportByUUID(uuid string) (Report, error) {
	coll := d.DB.Collection("reports")

	var report Report
	err := coll.FindOne(context.Background(), bson.D{{"uuid", uuid}}).Decode(&report)
	if err != nil {
		return Report{}, err
	}

	return report, nil
}

func (d *Database) AddReview(review Review) error {
	coll := d.DB.Collection("reviews")

	_, err := coll.InsertOne(context.Background(), review)
	return err
}
