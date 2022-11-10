export const IS_LOCAL = process.env.REACT_APP_ENV === 'local'

export const RPC_HOST = IS_LOCAL ? 'localhost' : 'informe.cloud'
export const RPC_PORT = '26548'
export const API_SERVER = IS_LOCAL ? "http://localhost:8080" : 'http://informe.cloud:8080'
