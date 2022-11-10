import React from 'react'
import GridHeader from './header'
import GridProper from './proper'
import { routeTo, request } from '../../util'
import { setCells, setDashboard } from '../../event/actions'
import store from "../../event/store"


export default class Grid extends React.Component {
    constructor(props) {
        super(props)

        this.getDashOrReset()
    }

    async getDashOrReset() {
        const dashUuid = this.getDashUuid()

        if (!dashUuid) {
            return routeTo("/dashboards")
        }

        const args = {id: dashUuid}
        const {data, err} = await request('dashboard/load', args)

        if (err) {
            return routeTo("/dashboards")
        }

        const resp = data.value
        const cells = resp.cells ? resp.cells : []


        store.dispatch(setCells(cells))
        store.dispatch(setDashboard(data))
    }

    getDashUuid() {
        const tokens = window.location.pathname.split('/')

        if (tokens.length === 3) {
            return tokens[2]
        }
    }

    render() {
        return (
            <span>
                <GridHeader />
                <GridProper />
            </span>
        )
    }
}
