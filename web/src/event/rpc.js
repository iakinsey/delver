import Connection from './conn'
import { sleep } from '../util'


const MAX_RETRIES = 10
const RETRY_DELAY = 5


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
        this.disablePreload()

        // Copy filter before mutation
        filter = JSON.parse(filter)
        filter.callback = uuid

        this.filters[uuid] = filter
        this.callbacks[uuid] = callback

        this.reconnect()
    }

    removeFilter(uuid) {
        if (this.filters[uuid]) {
            return
        }

        delete this.filters[uuid]
        delete this.callbacks[uuid]

        this.disablePreload()
        this.reconnect()
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

    reconnect() {
        if (this.conn) {
            this.conn.disconnect()
        }

        if (this.filters.length === 0) {
            return
        }

        this.conn = new Connection(
            this.getFilters(),
            this.onMessage.bind(this),
            this.onDisconnect.bind(this)
        )
        this.conn.connect()
    }

    async onDisconnect() {
        for (var i = 0; i < MAX_RETRIES; i++) {
            if (this.filters.length > 0) {
                return
            }

            try {
                this.reconnect()
                return
            } catch (e) {
                await sleep(RETRY_DELAY)
            }
        }
    }
}
