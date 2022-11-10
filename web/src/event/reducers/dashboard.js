import { SET_DASHBOARD, CLEAR_DASHBOARD } from "../actionTypes.js"


const initialState = {}
 

export default function(state = initialState, action) {
    switch (action.type) {
        case SET_DASHBOARD:
            return action.data
        case CLEAR_DASHBOARD:
            return {}
        default:
            return state
    }
}
