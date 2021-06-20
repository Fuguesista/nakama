import { component, useCallback, useEffect, useState } from "haunted"
import { html, nothing } from "lit-html"
import { ifDefined } from "lit-html/directives/if-defined"
import { repeat } from "lit-html/directives/repeat.js"
import { hasUnreadNotificationsStore, notificationsEnabledStore, setLocalNotificationsEnabled, useStore } from "../ctx.js"
import { request } from "../http.js"
import "./intersectable-comp.js"
import "./relative-datetime.js"
import "./toast-item.js"

const pageSize = 10

export default function () {
    return html`<notifications-page></notifications-page>`
}

function NotificationsPage() {
    const [notificationsEnabled, setNotificationsEnabled] = useStore(notificationsEnabledStore)
    const [notifications, setNotifications] = useState([])
    const [notificationsEndCursor, setNotificationsEndCursor] = useState(null)
    const [fetching, setFething] = useState(notifications.length === 0)
    const [err, setErr] = useState(null)
    const [loadingMore, setLoadingMore] = useState(false)
    const [noMoreNotifications, setNoMoreNotifications] = useState(false)
    const [endReached, setEndReached] = useState(false)
    const [queue, setQueue] = useState([])
    const [markingAllAsRead, setMarkingAllAsRead] = useState(false)
    const [_, setHasUnreadNotifications] = useStore(hasUnreadNotificationsStore)
    const [toast, setToast] = useState(null)

    const onNotifyInputChange = useCallback(ev => {
        ev.currentTarget.checked = false
        setNotificationsEnabled(v => {
            const checked = !v
            if (checked && Notification.permission === "denied") {
                setToast({ type: "error", content: "notification permissions denied" })
                setLocalNotificationsEnabled(false)
                return false
            } else if (checked && Notification.permission === "default") {
                Notification.requestPermission().then(perm => {
                    const val = perm === "granted"
                    setLocalNotificationsEnabled(val)
                    setNotificationsEnabled(() => val)
                }).catch(err => {
                    const msg = "could not request notification permissions: " + err.message
                    console.error(msg)
                    setToast({ type: "error", content: msg })
                    setLocalNotificationsEnabled(() => false)
                    return false
                })
                return checked
            }
            setLocalNotificationsEnabled(checked)
            return checked
        })
    }, [])

    const onNewNotificationArrive = useCallback(n => {
        setNotifications(nn => {
            if (nn.findIndex(notif => notif.id === n.id) === -1) {
                setQueue(nn => [n, ...nn])
                return nn
            }

            return nn.map(notif => notif.id === n.id ? ({
                ...notif,
                ...n,
            }) : notif)
        })
    }, [])

    const onQueueBtnClick = useCallback(() => {
        setNotifications(nn => [...queue, ...nn])
        setQueue([])
    }, [queue])

    const loadMore = useCallback(() => {
        if (loadingMore || noMoreNotifications) {
            return
        }

        setLoadingMore(true)
        fetchNotifications(notificationsEndCursor).then(({ items: notifications, endCursor }) => {
            setNotifications(nn => [...nn, ...notifications])
            setNotificationsEndCursor(endCursor)

            if (notifications.length < pageSize) {
                setNoMoreNotifications(true)
                setEndReached(true)
            }
        }, err => {
            const msg = "could not fetch more notifications: " + err.message
            console.error(msg)
            setToast({ type: "error", content: msg })
        }).finally(() => {
            setLoadingMore(false)
        })
    }, [loadingMore, noMoreNotifications, notificationsEndCursor])

    const onReadAllBtnClick = useCallback(() => {
        setMarkingAllAsRead(true)
        markAllNotificationsAsRead().then(() => {
            setNotifications(nn => nn.map(n => ({
                ...n,
                read: true,
            })))
            setHasUnreadNotifications(false)
        }, err => {
            const msg = "could not mark all notifications as read: " + err.message
            console.error(msg)
            setToast({ type: "error", content: msg })
        }).finally(() => {
            setMarkingAllAsRead(false)
        })
    }, [])

    useEffect(() => {
        setFething(true)
        fetchNotifications().then(({ items: notifications, endCursor }) => {
            setNotifications(notifications)
            setNotificationsEndCursor(endCursor)

            if (notifications.length < pageSize) {
                setNoMoreNotifications(true)
            }
        }, err => {
            console.error("could not fetch notifications:", err)
            setErr(err)
        }).finally(() => {
            setFething(false)
        })
    }, [])

    useEffect(() => subscribeToNotifications(onNewNotificationArrive), [])

    return html`
        <main class="container notifications-page">
            <div class="notifications-heading">
                <h1>Notifications</h1>
                <div class="notifications-controls">
                    <label class="switch-wrapper">
                        <input type="checkbox" role="switch" name="notifications_enabled" .checked=${notificationsEnabled} @change=${onNotifyInputChange}>
                        <span>Notify?</span>
                    </label>
                    <button .disabled=${markingAllAsRead} @click=${onReadAllBtnClick}>
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g data-name="Layer 2"><g data-name="checkmark-circle"><rect width="24" height="24" opacity="0"/><path d="M9.71 11.29a1 1 0 0 0-1.42 1.42l3 3A1 1 0 0 0 12 16a1 1 0 0 0 .72-.34l7-8a1 1 0 0 0-1.5-1.32L12 13.54z"/><path d="M21 11a1 1 0 0 0-1 1 8 8 0 0 1-8 8A8 8 0 0 1 6.33 6.36 7.93 7.93 0 0 1 12 4a8.79 8.79 0 0 1 1.9.22 1 1 0 1 0 .47-1.94A10.54 10.54 0 0 0 12 2a10 10 0 0 0-7 17.09A9.93 9.93 0 0 0 12 22a10 10 0 0 0 10-10 1 1 0 0 0-1-1z"/></g></g></svg>
                        <span>Read all</span>
                    </button>
                </div>
            </div>
            ${err !== null ? html`
                <p class="error" role="alert">Could not fetch notifications: ${err.message}</p>
            ` : fetching ? html`
                <p class="loader" aria-busy="true" aria-live="polite">Loading notifications... please wait.<p>
            ` : html`
                ${queue.length !== 0 ? html`
                    <button class="queue-btn" @click=${onQueueBtnClick}>${queue.length} new notifications</button>
                ` : nothing}
                ${notifications.length === 0 ? html`
                    <p>0 notifications</p>
                ` : html`
                    <div class="notifications" role="feed">
                        ${repeat(notifications, n => n.id, n => html`<notification-item .notification=${n}></notification-item>`)}
                    </div>
                    ${!noMoreNotifications ? html`
                        <intersectable-comp @is-intersecting=${loadMore}></intersectable-comp>
                        <p class="loader" aria-busy="true" aria-live="polite">Loading notifications... please wait.<p>
                    ` : endReached ? html`
                        <p>End reached.</p>
                    ` : nothing}
                `}
            `}
        </main>
        ${toast !== null ? html`<toast-item .toast=${toast}></toast-item>` : nothing}
    `
}

customElements.define("notifications-page", component(NotificationsPage, { useShadowDOM: false }))



function NotificationItem({ notification: initialNotification }) {
    const [_, setHasUnreadNotifications] = useStore(hasUnreadNotificationsStore)
    const [notification, setNotification] = useState(initialNotification)
    const [fetching, setFetching] = useState(false)
    const [toast, setToast] = useState(null)

    const getActors = () => {
        const aa = notification.actors
        switch (aa.length) {
            case 0:
                return "Someone"
            case 1:
                return html`<a href="/@${aa[0]}">${aa[0]}</a>`
            case 2:
                return html`<a href="/@${aa[0]}">${aa[0]}</a> and <a href="/@${aa[1]}">${aa[1]}</a>`
            default:
                return notification.type === "follow"
                    ? html`${repeat(aa.slice(0, aa.length - 1), u => u, (u, i) => html`${i > 0 ? ", " : ""}<a href="/@${u}">${u}</a>`)} and <a href="/@${aa[aa.length - 1]}">${aa[aa.length - 1]}</a>`
                    : html`<a href="/@${aa[0]}">${aa[0]}</a> and ${aa.length - 1} others`
        }
    }

    const getAction = () => {
        switch (notification.type) {
            case "follow":
                return "followed you"
            case "comment":
                return html`commented in a <a href="/posts/${notification.postID}">post</a>`
            case "post_mention":
                return html`mentioned you in a <a href="/posts/${notification.postID}">post</a>`
            case "comment_mention":
                return html`mentioned you in a <a href="/posts/${notification.postID}">comment</a>`
            default:
                return "did something"
        }
    }

    const onClick = useCallback(() => {
        setFetching(true)
        markNotificationAsRead(notification.id).then(() => {
            setNotification(n => ({
                ...n,
                read: true,
            }))
        }, err => {
            const msg = "could not mark notification as read: " + err.message
            console.error(msg)
            setToast({ type: "error", content: msg })
        }).finally(() => {
            setFetching(false)
        }).then(() => {
            fetchHasUnreadNotifications().then(setHasUnreadNotifications, err => {
                console.error("could not fetch has unread notifications:", err)
            })
        })
    }, [notification])

    // For CSS.
    useEffect(() => {
        if (notification.read) {
            this.setAttribute("read", "")
        } else {
            this.removeAttribute("read")
        }
    }, [notification])

    useEffect(() => {
        setNotification(initialNotification)
    }, [initialNotification])

    return html`
        <div class="notification" style=${ifDefined(fetching ? "pointer-events: none;" : undefined)} @click=${onClick}>
            <div>
                <p>${getActors()} ${getAction()}.</p>
                <relative-datetime .datetime=${notification.issuedAt}></relative-datetime>
            </div>
            ${!notification.read ? html`
                <button .disabled=${fetching}>
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g data-name="Layer 2"><g data-name="checkmark-circle"><rect width="24" height="24" opacity="0"/><path d="M9.71 11.29a1 1 0 0 0-1.42 1.42l3 3A1 1 0 0 0 12 16a1 1 0 0 0 .72-.34l7-8a1 1 0 0 0-1.5-1.32L12 13.54z"/><path d="M21 11a1 1 0 0 0-1 1 8 8 0 0 1-8 8A8 8 0 0 1 6.33 6.36 7.93 7.93 0 0 1 12 4a8.79 8.79 0 0 1 1.9.22 1 1 0 1 0 .47-1.94A10.54 10.54 0 0 0 12 2a10 10 0 0 0-7 17.09A9.93 9.93 0 0 0 12 22a10 10 0 0 0 10-10 1 1 0 0 0-1-1z"/></g></g></svg>
                    <span>Read</span>
                </button>
            ` : nothing}
        </div>
        ${toast !== null ? html`<toast-item .toast=${toast}></toast-item>` : nothing}
    `
}

// @ts-ignore
customElements.define("notification-item", component(NotificationItem, { useShadowDOM: false }))

function fetchNotifications(before = "", last = pageSize) {
    return request("GET", `/api/notifications?last=${encodeURIComponent(last)}&before=${encodeURIComponent(before)}`)
        .then(resp => resp.body)
        .then(page => {
            page.items = page.items.map(n => ({
                ...n,
                issuedAt: new Date(n.issuedAt),
            }))
            return page
        })
}

function subscribeToNotifications(cb) {
    const handler = ev => {
        cb(ev.detail)
    }

    addEventListener("new-notification-arrived", handler)
    return () => {
        removeEventListener("new-notification-arrived", handler)
    }
}

function markNotificationAsRead(notificationID) {
    return request("POST", `/api/notifications/${encodeURIComponent(notificationID)}/mark_as_read`)
        .then(() => void 0)
}

function markAllNotificationsAsRead() {
    return request("POST", "/api/mark_notifications_as_read")
        .then(() => void 0)
}

function fetchHasUnreadNotifications() {
    return request("GET", "/api/has_unread_notifications")
        .then(resp => resp.body)
        .then(v => Boolean(v))
}
