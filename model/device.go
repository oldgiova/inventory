// Copyright 2020 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package model

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	AttrScopeInventory = "inventory"
	AttrScopeIdentity  = "identity"
	AttrScopeSystem    = "system"

	AttrNameGroup   = "group"
	AttrNameUpdated = "updated_ts"
	AttrNameCreated = "created_ts"
)

type DeviceID string

var NilDeviceID DeviceID //TODO: how to make it NilDeviceID:=DeviceID(primitive.NilObjectID)

type GroupName string

type DeviceAttribute struct {
	Name        string      `json:"name" bson:",omitempty"`
	Description *string     `json:"description,omitempty" bson:",omitempty"`
	Value       interface{} `json:"value" bson:",omitempty"`
	Scope       string      `json:"scope" bson:",omitempty"`
}

func (da DeviceAttribute) Validate() error {
	return validation.ValidateStruct(&da,
		validation.Field(&da.Name, validation.Required, validation.Length(1, 1024)),
		validation.Field(&da.Scope, validation.Required, validation.Length(1, 1024)),
		validation.Field(&da.Value, validation.By(validateDeviceAttrVal)),
	)
}

func validateDeviceAttrVal(i interface{}) error {
	if i == nil {
		return errors.New("supported types are string, float64, and arrays thereof")
	}
	rType := reflect.TypeOf(i)
	if rType.Kind() == reflect.Interface {
		rType = rType.Elem()
	}

	switch rType.Kind() {
	case reflect.Float64, reflect.String:
		return nil
	case reflect.Slice:
		elemKind := rType.Elem().Kind()
		if elemKind == reflect.Float64 || elemKind == reflect.String {
			return nil
		} else if elemKind == reflect.Interface {
			return validateDeviceAttrValArray(i)
		}
	}
	return errors.New("supported types are string, float64, and arrays thereof")
}

func validateDeviceAttrValArray(arr interface{}) error {
	rVal := reflect.ValueOf(arr)
	rLen := rVal.Len()
	if rLen == 0 {
		return nil
	}
	elem := rVal.Index(0)
	kind := elem.Kind()
	if elem.Kind() == reflect.Interface {
		elem = elem.Elem()
		kind = elem.Kind()
	}
	if kind != reflect.String && kind != reflect.Float64 {
		return errors.New(
			"array values must be either string or float64, not: " +
				kind.String())
	}
	for i := 1; i < rLen; i++ {
		elem = rVal.Index(i)
		elemKind := elem.Kind()
		if elemKind == reflect.Interface {
			elemKind = elem.Elem().Kind()
		}
		if elemKind != kind {
			return errors.New(
				"array values must be of consistent type: " +
					"string or float64",
			)
		}
	}
	return nil
}

// Device wrapper
type Device struct {
	//system-generated device ID
	ID DeviceID `json:"id" bson:"_id,omitempty"`

	//a map of attributes names and their values.
	Attributes DeviceAttributes `json:"attributes,omitempty" bson:"attributes,omitempty"`

	//device's group name
	Group GroupName `json:"-" bson:"group,omitempty"`

	CreatedTs time.Time `json:"-" bson:"created_ts,omitempty"`
	//Timestamp of the last attribute update.
	UpdatedTs time.Time `json:"updated_ts" bson:"updated_ts,omitempty"`
}

// internalDevice is only used internally to avoid recursive type-loops for
// member functions.
type internalDevice Device

func (d *Device) UnmarshalBSON(b []byte) error {
	if err := bson.Unmarshal(b, (*internalDevice)(d)); err != nil {
		return err
	}
	for _, attr := range d.Attributes {
		if attr.Scope == AttrScopeSystem {
			switch attr.Name {
			case AttrNameGroup:
				group := attr.Value.(string)
				d.Group = GroupName(group)
			case AttrNameUpdated:
				dateTime := attr.Value.(primitive.DateTime)
				d.UpdatedTs = dateTime.Time()
			case AttrNameCreated:
				dateTime := attr.Value.(primitive.DateTime)
				d.CreatedTs = dateTime.Time()
			}
		}
	}
	return nil
}

func (d Device) MarshalBSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.Group != "" {
		d.Attributes = append(d.Attributes, DeviceAttribute{
			Scope: AttrScopeSystem,
			Name:  AttrNameGroup,
			Value: d.Group,
		})
	}
	return bson.Marshal(internalDevice(d))
}

func (d Device) Validate() error {
	return validation.ValidateStruct(&d,
		validation.Field(&d.ID, validation.Required, validation.Length(1, 1024)),
		validation.Field(&d.Attributes),
	)
}

func (did DeviceID) String() string {
	return string(did)
}

func (gn GroupName) String() string {
	return string(gn)
}

func (gn GroupName) Validate() error {
	if len(gn) > 1024 {
		return errors.New(
			"Group name can at most have 1024 characters",
		)
	} else if len(gn) == 0 {
		return errors.New(
			"Group name cannot be blank",
		)
	} else if ok, _ := regexp.MatchString(
		"^[A-Za-z0-9_-]*$", string(gn),
	); !ok {
		return errors.New(
			"Group name can only contain: upper/lowercase " +
				"alphanum, -(dash), _(underscore)",
		)
	}
	return nil
}

// wrapper for device attributes names and values
type DeviceAttributes []DeviceAttribute

func (d *DeviceAttributes) UnmarshalJSON(b []byte) error {
	err := json.Unmarshal(b, (*[]DeviceAttribute)(d))
	if err != nil {
		return err
	}
	for i := range *d {
		if (*d)[i].Scope == "" {
			(*d)[i].Scope = AttrScopeInventory
		}
	}

	return nil
}

// MarshalJSON ensures that an empty array is returned if DeviceAttributes is
// empty.
func (d DeviceAttributes) MarshalJSON() ([]byte, error) {
	if d == nil {
		return json.Marshal([]DeviceAttribute{})
	}
	return json.Marshal([]DeviceAttribute(d))
}

func (d DeviceAttributes) Validate() error {
	for _, a := range d {
		if err := a.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// UnmarshalBSONValue correctly unmarshals DeviceAttributes from Device
// documents stored in the DB.
func (d *DeviceAttributes) UnmarshalBSONValue(t bsontype.Type, b []byte) error {
	raw := bson.Raw(b)
	elems, err := raw.Elements()
	if err != nil {
		return err
	}
	*d = make(DeviceAttributes, len(elems))
	for i, elem := range elems {
		err = elem.Value().Unmarshal(&(*d)[i])
		if err != nil {
			return err
		}
	}

	return nil
}

// MarshalBSONValue marshals the DeviceAttributes to a mongo-compatible
// document. That is, each attribute is given a unique field consisting of
// "<scope>-<name>".
func (d DeviceAttributes) MarshalBSONValue() (bsontype.Type, []byte, error) {
	attrs := make(bson.D, len(d))
	replacer := strings.NewReplacer(".", "_", "$", "_S_")
	for i := range d {
		d[i].Name = replacer.Replace(d[i].Name)
		attrs[i].Key = d[i].Scope + "-" + d[i].Name
		attrs[i].Value = &d[i]
	}
	return bson.MarshalValue(attrs)
}
