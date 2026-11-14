import Connection from './conn'
import { sleep } from '../util'


const MAX_RETRIES = 10
const RETRY_DELAY = 5000


const ERR_NO_CALLBACK_UUID = 'No callback UUID specified.'
const ERR_INVALID_CALLBACK = 'Callback UUID does not exist.'


export default class RPC {
    constructor() {
        // Quick hack to assure filter is a list
        this.filters = {}
        this.callbacks = {}
        this.connected = false
        this.conn = null
    }

    getFilters() {
        return JSON.stringify(Object.values(this.filters))
    }

    addFilter(uuid, filter, callback) {

        // Copy filter before mutation
        filter = JSON.parse(filter)
        filter.callback = uuid

        this.filters[uuid] = filter
        this.callbacks[uuid] = callback

        this.reconnect().catch(() => this.onDisconnect())
    }

    removeFilter(uuid) {
        if (!this.filters[uuid]) {
            return
        }

        delete this.filters[uuid]
        delete this.callbacks[uuid]

        this.reconnect().catch(() => this.onDisconnect())
    }

    onMessage(msg) {
        // XXX Typically the entire message chunk will contain the same UUID.
        // This could change in the future.

        if (!msg || !msg.data || msg.data.length === 0) {
            return
        }

        this.handleEntity(msg, msg.data[0].callback)
    }

    handleEntity(entity, callbackId) {
        if (!callbackId) {
            throw ERR_NO_CALLBACK_UUID
        } else if (!this.callbacks[callbackId]) {
            throw ERR_INVALID_CALLBACK
        }
 
        const callback = this.callbacks[callbackId]

        callback(entity)
    }

    disablePreload() {
        for (let filter of Object.values(this.filters)) {
            var options = filter.options ? filter.options : {}

            options.preload = false
            filter.options = options
        }
    }

    async reconnect() {
        if (this.conn) {
            this.conn.disconnect()
            this.conn = null
        }

        if (Object.keys(this.filters).length === 0) {
            return
        }

        const connection = new Connection(
            this.getFilters(),
            message => { if (this.conn === connection) this.onMessage(message) },
            error => { if (this.conn === connection) this.onDisconnect(error) }
        )
        this.conn = connection
        return await connection.connect()
    }

    async onDisconnect() {
        if (this.retrying) return
        this.retrying = true
        try {
            for (let i = 0; i < MAX_RETRIES; i++) {
                if (Object.keys(this.filters).length === 0) return
                try { await this.reconnect(); return } catch (e) { await sleep(RETRY_DELAY) }
            }
        } finally { this.retrying = false }
    }
}
