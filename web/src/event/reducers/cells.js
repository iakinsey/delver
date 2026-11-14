import {
    SET_CELL, SET_CELLS, UPDATE_CELL, CLEAR_CELLS, DELETE_CELL
} from "../actionTypes.js"


const initialState = {}
 

export default function(state = initialState, action) {
    switch (action.type) {
        case SET_CELL:
            return {...state, [action.data.uuid]: action.data}
        case UPDATE_CELL:
            const cell = state[action.data.uuid]
            const newCell = Object.assign({}, cell, action.data)
            return {...state, [action.data.uuid]: newCell}
        case SET_CELLS:
            const result = {}

            for (const cell of action.data) {
                result[cell.uuid] = cell
            }

            return result
        case DELETE_CELL:
            const remaining = {...state}
            delete remaining[action.data]
            return remaining
        case CLEAR_CELLS:
            return {}
        default:
            return state
    }
}
