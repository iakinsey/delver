import { combineReducers } from "redux"
import cells from "./cells"
import windows from "./windows"
import dashboard from './dashboard'

export default combineReducers({
    cells,
    windows,
    dashboard
})
