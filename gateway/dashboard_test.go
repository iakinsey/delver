package gateway

import (
	"encoding/json"
	"testing"

	"github.com/iakinsey/delver/types"
	"github.com/stretchr/testify/assert"
)

func genDash() types.Dashboard {
	dVal := json.RawMessage{}
	value := map[string]string{
		"k1": string(types.NewV4()),
		"k2": string(types.NewV4()),
		"k3": string(types.NewV4()),
	}

	b, _ := json.Marshal(value)

	dVal.UnmarshalJSON(b)

	return types.Dashboard{
		ID:          string(types.NewV4()),
		Name:        string(types.NewV4()),
		UserID:      string(types.NewV4()),
		Description: string(types.NewV4()),
		Value:       dVal,
	}
}

func TestCreateAndGetDash(t *testing.T) {
	gateway := NewDashboardGateway(":memory:")
	dash := genDash()

	assert.NoError(t, gateway.Put(dash))

	dash2, err := gateway.Get(dash.UserID, dash.ID)

	assert.NoError(t, err)
	assert.EqualValues(t, dash, *dash2)
}

func TestGetDashUnauthorized(t *testing.T) {
	gateway := NewDashboardGateway(":memory:")
	dash := genDash()

	assert.NoError(t, gateway.Put(dash))

	dash2, err := gateway.Get(string(types.NewV4()), dash.ID)

	assert.Nil(t, dash2)
	assert.EqualError(t, err, "Unauthorized")
}

func TestGetNoDashExists(t *testing.T) {
	gateway := NewDashboardGateway(":memory:")
	dash, err := gateway.Get(string(types.NewV4()), string(types.NewV4()))

	assert.Nil(t, dash)
	assert.EqualError(t, err, "Dashboard does not exist")
}

func TestListDash(t *testing.T) {
	gateway := NewDashboardGateway(":memory:")
	count := 10
	userID := string(types.NewV4())

	for i := 0; i < count; i++ {
		dash := genDash()
		dash.UserID = userID

		assert.NoError(t, gateway.Put(dash))
	}

	l, err := gateway.List(userID)

	assert.NoError(t, err)
	assert.Len(t, l, count)
}

func TestDeleteDash(t *testing.T) {
	gateway := NewDashboardGateway(":memory:")
	dash := genDash()

	assert.NoError(t, gateway.Put(dash))

	dash2, err := gateway.Get(dash.UserID, dash.ID)

	assert.NoError(t, err)
	assert.EqualValues(t, dash, *dash2)
	assert.NoError(t, gateway.Delete(dash.UserID, dash.ID))

	dash3, err := gateway.Get(dash.UserID, dash.ID)

	assert.Nil(t, dash3)
	assert.EqualError(t, err, "Dashboard does not exist")
}

func TestDeleteDashNoDashExists(t *testing.T) {
	gateway := NewDashboardGateway(":memory:")
	dash := genDash()
	err := gateway.Delete(dash.UserID, dash.ID)

	assert.EqualError(t, err, "Dashboard does not exist")
}
