import { WINDOW_RESIZE } from "../actionTypes.js"


const getWindowSize = () => ({
    width: window.innerWidth,
    height: window.innerHeight
})


export default function(state = getWindowSize(), action) {
    switch (action.type) {
        case WINDOW_RESIZE:
            return getWindowSize()
        default:
            return state
    }
}
