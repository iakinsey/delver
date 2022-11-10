import { RPC_HOST, RPC_PORT } from '../config'


export default class Connection {
    constructor(filters, onMessage, onDisconnect) {
        // Quick hack to assure filter is a list
        this.filters = filters
        this.onMessage = onMessage
        this.onDisconnect = onDisconnect
        this.host = RPC_HOST
        this.port = RPC_PORT
        this.connected = false
        this.ws = null
    }

    disconnect() {
        if (this.connected) {
            this.ws.close()
        }
    }

    connect() {
        const filter = btoa(this.filters).replace('/', '_')
        
        const url = "ws://" + this.host + ":" + this.port + '/' + filter
        this.ws = new WebSocket(url)

        return new Promise((resolve, reject) => {
            this.ws.onopen = () => { this.onOpen(this.ws, resolve) }
            this.ws.onerror = (error) => { this.onError(error, reject) }
        })
    }

    close() {
        this.ws.close()
    }

    onOpen(ws, resolve) {
        this.connected = true
        ws.onmessage = (msg) => { this.handleMessage(msg) }
        resolve(ws)
    }

    onError(error, reject) {
        if (this.connected && this.onDisconnect) {
            this.onDisconnect(error)
        } else {
            reject(error)
        }
    }

    handleMessage(event) {
        const message = JSON.parse(event.data)
        this.onMessage(message)
    }

    send(message) {
        this.ws.send(JSON.stringify(message))
    }
}
