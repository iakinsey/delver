import md5 from 'md5'
import { API_SERVER } from "./config"


export function validateJSON(str) {
    try {
        JSON.parse(str);
    } catch (e) {
        return String(e).split(':').pop(-1).trim()
    }
}


export function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}


export function uuid4() {
    return ([1e7]+-1e3+-4e3+-8e3+-1e11).replace(/[018]/g, c =>
        // eslint-disable-next-line
        (c ^ crypto.getRandomValues(new Uint8Array(1))[0] & 15 >> c / 4).toString(16)
    );
}


export function getKey(prefix, dictionary) {
    return md5(prefix + JSON.stringify(dictionary))
}


export function routeTo(url) {
    window.location.href = url
}


export function getUserInfo() {
    return {
        email: localStorage.email,
        auth: localStorage.auth
    }
}
export function setUserInfo(email, auth) {
    localStorage.email = email 
    localStorage.auth = auth
}


export function hasUserInfo() {
    return localStorage.email && localStorage.auth
}


export async function request(url, data) {
    if (!hasUserInfo()) {
        routeTo('/login')
        return
    }

    const requestUrl = `${API_SERVER}/${url}`

    const response = await fetch(requestUrl, {
        method: "POST",
        body: JSON.stringify(data),
        headers: {
            'Content-Type': 'application/json',
            auth: localStorage.auth
        }
    })

    return await response.json()
}


export async function logout() {
    await request('user/logout', {})
    delete localStorage.email
    delete localStorage.auth

    routeTo('/')
}

export function getDateQueryString(d) {
    const datePart = [
        d.getFullYear(),
        String(d.getMonth() + 1).padStart(2, "0"),
        String(d.getDate()).padStart(2, "0")
    ].join('-')

    const timePart = [
        String(d.getHours()).padStart(2, "0"),
        String(d.getMinutes()).padStart(2, "0"),
        String(d.getMinutes()).padStart(2, "0")
    ].join(":")

    return `${datePart} ${timePart}`
}
