import { RPC_HOST, RPC_PORT } from '../config'


export default class Connection {
    constructor(filters, onMessage, onDisconnect) {
        // Quick hack to assure filter is a list
        this.filters = filters
        this.onMessage = onMessage
        this.onDisconnect = onDisconnect
        this.host = RPC_HOST
        this.port = RPC_PORT
        this.intentionalClose = false
        this.connected = false
        this.ws = null
    }

    disconnect() {
        this.intentionalClose = true
        if (this.ws) {
            this.ws.close()
        }
    }

    connect() {
        const filter = btoa(unescape(encodeURIComponent(this.filters))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
        
        const protocol = window.location.protocol === "https:" ? "wss://" : "ws://"
        const url = protocol + this.host + ":" + this.port + '/' + filter
        this.ws = new WebSocket(url)

        return new Promise((resolve, reject) => {
            this.ws.onclose = () => {
                const wasConnected = this.connected
                this.connected = false
                if (!wasConnected) { reject(new Error("Connection closed before opening")); return }
                if (!this.intentionalClose && this.onDisconnect) this.onDisconnect()
            }
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
            this.ws.close()
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
