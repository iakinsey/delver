import {
    SET_CELL, SET_CELLS, UPDATE_CELL, CLEAR_CELLS, DELETE_CELL
} from "../actionTypes.js"


const initialState = {}
 

export default function(state = initialState, action) {
    switch (action.type) {
        case SET_CELL:
            state[action.data.uuid] = action.data
        
            return Object.assign({}, state)
        case UPDATE_CELL:
            const cell = state[action.data.uuid]
            const newCell = Object.assign({}, cell, action.data)
            state[action.data.uuid] = newCell

            return Object.assign({}, state)
        case SET_CELLS:
            const result = {}

            for (const cell of action.data) {
                result[cell.uuid] = cell
            }

            return result
        case DELETE_CELL:
            delete state[action.data]

            return state
        case CLEAR_CELLS:
            return {}
        default:
            return state
    }
}
