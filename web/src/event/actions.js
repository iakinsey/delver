import {
    SET_CELL,
    UPDATE_CELL,
    CLEAR_CELLS,
    DELETE_CELL,
    WINDOW_RESIZE,
    SET_CELLS,
    SET_DASHBOARD,
    CLEAR_DASHBOARD
} from './actionTypes'


export const setCell = (cell) => ({
    type: SET_CELL,
    data: cell
})


export const updateCell = (cell) => ({
    type: UPDATE_CELL,
    data: cell
})


export const clearCells = () => ({
    type: CLEAR_CELLS
})


export const deleteCell = (uuid) => ({
    type: DELETE_CELL,
    data: uuid
})

export const notifyWindow = () => ({
    type: WINDOW_RESIZE,
    data: true
})


export const setCells = (cells) => ({
    type: SET_CELLS,
    data: cells
})


export const setDashboard = (dashboard) => ({
    type: SET_DASHBOARD,
    data: dashboard
})

export const clearDashboard = (dashboard) => ({
    type: CLEAR_DASHBOARD,
    data: dashboard
})
