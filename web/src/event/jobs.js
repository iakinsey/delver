import store from "./store"
import { notifyWindow } from './actions'


function startJobs() {
    window.addEventListener("resize", () => {
        store.dispatch(notifyWindow())
    })
}

export default startJobs
