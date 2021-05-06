import { isAuthenticated } from "../auth.js"
import { doDelete, doPost } from "../http.js"
import { ago, collectMedia, el, escapeHTML, linkify, replaceNode } from "../utils.js"
import renderAvatarHTML from "./avatar.js"
import { heartIconSVG, heartOulineIconSVG } from "./icons.js"

const messageIconSVG = `<svg class="icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g data-name="Layer 2"><g data-name="message-square"><rect width="24" height="24" opacity="0"/><circle cx="12" cy="11" r="1"/><circle cx="16" cy="11" r="1"/><circle cx="8" cy="11" r="1"/><path d="M19 3H5a3 3 0 0 0-3 3v15a1 1 0 0 0 .51.87A1 1 0 0 0 3 22a1 1 0 0 0 .51-.14L8 19.14a1 1 0 0 1 .55-.14H19a3 3 0 0 0 3-3V6a3 3 0 0 0-3-3zm1 13a1 1 0 0 1-1 1H8.55a3 3 0 0 0-1.55.43l-3 1.8V6a1 1 0 0 1 1-1h14a1 1 0 0 1 1 1z"/></g></g></svg>`

/**
 * @param {import("../types.js").Post} post
 * @param {string=} timelineItemID
 */
export default function renderPost(post, timelineItemID = null) {
    const authenticated = isAuthenticated()
    const { user } = post
    const content = linkify(escapeHTML(post.content))

    const article = document.createElement("article")
    article.className = "micro-post"
    article.setAttribute("aria-label", `${user.username}'s post`)
    article.innerHTML = /*html*/`
        <div class="micro-post-header">
            <a class="micro-post-user" href="/users/${user.username}">
                ${renderAvatarHTML(user)}
                <span>${user.username}</span>
            </a>
            <a class="micro-post-ts" href="/posts/${post.id}">
                <time datetime="${post.createdAt}">${ago(post.createdAt)}</time>
            </a>
        </div>
        <div class="micro-post-content">
            <p>${content}</p>
        </div>
        <div class="micro-post-controls">
            ${authenticated ? `
                <button class="like-button"
                    title="${post.liked ? "Unlike" : "Like"}"
                    aria-pressed="${post.liked}"
                    aria-label="${post.likesCount} likes">
                    <span class="likes-count">${post.likesCount}</span>
                    ${post.liked ? heartIconSVG : heartOulineIconSVG}
                </button>
            ` : `
                <span class="likes-count-wrapper" aria-label="${post.likesCount} likes">
                    <span>${post.likesCount}</span>
                    ${heartOulineIconSVG}
                </span>
            `}
            <a class="comments-link"
                href="/posts/${post.id}"
                title="Comments"
                aria-label="${post.commentsCount} comments">
                <span class="comments-count">${post.commentsCount}</span>
                ${messageIconSVG}
            </a>
            ${authenticated ? `
                <div class="menu-wrapper">
                    <button class="menu-btn" title="More" id="post-more-menu-btn-${post.id}" aria-haspopup="true" aria-controls="post-more-menu-${post.id}">
                        <svg class="icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><g data-name="Layer 2"><g data-name="more-horizotnal"><rect width="24" height="24" opacity="0"/><circle cx="12" cy="12" r="2"/><circle cx="19" cy="12" r="2"/><circle cx="5" cy="12" r="2"/></g></g></svg>
                    </button>
                    <ul class="menu" id="post-more-menu-${post.id}" role="menu" aria-labelledby="post-more-menu-btn-${post.id}" tabindex="-1" style="display: none;">
                        ${post.mine ? `
                        <li role="none">
                            <button role="menuitem" tabindex="-1" class="edit-btn">Edit</button>
                        </li>
                        ` : ""}
                        <li>
                            <button role="menuitem" tabindex="-1" aria-pressed="${post.subscribed}" class="subscription-toggle-btn">${post.subscribed ? "Unsubscribe" : "Subscribe"}</button>
                        </li>
                        ${timelineItemID !== null ? `
                        <li>
                            <button role="menuitem" tabindex="-1" class="remove-btn">Remove</button>
                        </li>
                        ` : ""}
                        ${post.mine ? `
                        <li>
                            <button role="menuitem" tabindex="-1" class="delete-btn">Delete</button>
                        </li>
                        ` : ""}
                    </ul>
                </div>
            ` : ""
        }
        </div >
    `

    const contentEl = article.querySelector(".micro-post-content")
    void async function (target) {
        const els = await collectMedia(target)
        for (const el of els) {
            contentEl.appendChild(el)
        }
    }(contentEl.querySelector("p"))

    const likeButton = /** @type {HTMLButtonElement=} */ (article.querySelector(".like-button"))
    if (likeButton !== null) {
        const likesCountEl = likeButton.querySelector(".likes-count")

        const onLikeButtonClick = async () => {
            if (typeof navigator.vibrate === "function") {
                navigator.vibrate([50])
            }

            likeButton.disabled = true
            try {
                const out = await togglePostLike(post.id)

                post.likesCount = out.likesCount
                post.liked = out.liked

                likeButton.title = out.liked ? "Unlike" : "Like"
                likeButton.setAttribute("aria-pressed", String(out.liked))
                likeButton.setAttribute("aria-label", out.likesCount + " likes")
                replaceNode(
                    likeButton.querySelector("svg"),
                    el(out.liked ? heartIconSVG : heartOulineIconSVG),
                )
                likesCountEl.textContent = String(out.likesCount)

                dispatchEvent(new CustomEvent("postlikecountchange", {
                    detail: { postID: post.id, ...out },
                }))
            } catch (err) {
                console.error(err)
                alert(err.message)
            } finally {
                likeButton.disabled = false
            }
        }

        likeButton.addEventListener("click", onLikeButtonClick)
    }

    const menuWrapper = article.querySelector(".menu-wrapper")
    const menuBtn = /** @type {HTMLButtonElement} */ (article.querySelector(".menu-btn"))
    const menu = /** @type {HTMLUListElement} */ (article.querySelector(".menu"))
    const menuItems = menu.querySelectorAll("[role=menuitem]")
    const menuItemsLength = menuItems.length
    const editBtn = menu.querySelector(".edit-btn")
    const subscriptionToggleBtn = menu.querySelector(".subscription-toggle-btn")
    const removeBtn = menu.querySelector(".remove-btn")
    const deleteBtn = menu.querySelector(".delete-btn")
    let menuExpanded = false
    let menuIdx = -1

    /**
     * @param {boolean} newVal
     */
    const setMenuExpanded = newVal => {
        menuExpanded = newVal
        if (menuExpanded) {
            menuBtn.setAttribute("aria-expanded", String(menuExpanded))
        } else {
            menuBtn.removeAttribute("aria-expanded")
        }
        menu.style.display = menuExpanded ? "block" : "none"
    }

    /**
     * @param {number} newVal
     */
    const setMenuIdx = newVal => {
        menuIdx = newVal
        const child = /** @type {HTMLLIElement} */ (menu.children.item(menuIdx))
        if (child === null) {
            return
        }

        const menuItem = child.getAttribute("role") === "menuitem" ? child : /** @type {HTMLElement} */ (child.querySelector("[role=menuitem]"))
        if (menuItem === null) {
            return
        }

        menuItem.focus()
    }

    /**
     * @param {Event} ev
     */
    const onMenuBtnClick = ev => {
        setTimeout(() => {
            if (document.activeElement !== null && !menu.contains(document.activeElement)) {
                setMenuExpanded(!menuExpanded)
                if (menuExpanded) {
                    setMenuIdx(0)
                }
            }
        })
    }

    const onBlur = () => {
        setTimeout(() => {
            if (document.activeElement !== null && !menuWrapper.contains(document.activeElement)) {
                setMenuExpanded(false)
            }
        })
    }

    /**
     * @param {KeyboardEvent} ev
     */
    const onMenuKeyDown = ev => {
        if (ev.key === "Enter") {
            if (document.activeElement !== null && menu.contains(document.activeElement)) {
                const menuItem = /** @type {HTMLElement} */ (document.activeElement)
                menuItem.click()
            }
            setMenuExpanded(false)
            menuBtn.focus()
            ev.preventDefault()
            ev.stopPropagation()
            return
        }

        if (ev.key === "Escape") {
            setMenuExpanded(false)
            menuBtn.focus()
            ev.preventDefault()
            ev.stopPropagation()
            return
        }

        if (ev.key === "ArrowUp") {
            // decrease or go to last if already first
            setMenuIdx(menuIdx - 1 === -1 ? menuItemsLength - 1 : menuIdx - 1)
            ev.preventDefault()
            ev.stopPropagation()
            return
        }

        if (ev.key === "ArrowDown") {
            // increse or go to first if already last
            setMenuIdx(menuIdx + 1 === menuItemsLength ? 0 : menuIdx + 1)
            ev.preventDefault()
            ev.stopPropagation()
            return
        }

        if (ev.key === "Home") {
            setMenuIdx(0)
            ev.preventDefault()
            ev.stopPropagation()
            return
        }

        if (ev.key === "End") {
            setMenuIdx(menuItemsLength - 1)
            ev.preventDefault()
            ev.stopPropagation()
            return
        }
    }

    /**
     * @param {Event} ev
     */
    const onMenuItemClick = ev => {
        const idx = Array.from(menuItems).findIndex(item => item === ev.currentTarget)
        if (idx === -1) {
            return
        }

        setMenuIdx(idx)
    }

    menuBtn.addEventListener("click", onMenuBtnClick)
    menuBtn.addEventListener("blur", onBlur)
    menu.addEventListener("keydown", onMenuKeyDown)
    for (const menuItem of menuItems) {
        menuItem.addEventListener("click", onMenuItemClick)
        menuItem.addEventListener("blur", onBlur)
    }

    const onEditBtnClick = ev => {
        alert("not implemented yet")
    }

    const onSubscriptionToggleBtnClick = ev => {
        togglePostSubscription(post.id).then(out => {
            post.subscribed = out.subscribed
            subscriptionToggleBtn.setAttribute("aria-pressed", String(out.subscribed))
            subscriptionToggleBtn.textContent = out.subscribed ? "Unsubscribe" : "Subscribe"
        })
    }

    const onRemoveBtnClick = ev => {
        removeTimelineItem(timelineItemID).then(() => {
            article.remove()
        }).catch(err => {
            console.error(err)
            alert(err.message)
        })
    }

    const onDeleteBtnClick = ev => {
        deletePost(post.id).then(() => {
            article.remove()
        }).catch(err => {
            console.error(err)
            alert(err.message)
        })
    }


    if (authenticated) {
        if (post.mine) {
            editBtn.addEventListener("click", onEditBtnClick)
            deleteBtn.addEventListener("click", onDeleteBtnClick)
        }

        subscriptionToggleBtn.addEventListener("click", onSubscriptionToggleBtnClick)

        if (timelineItemID !== null) {
            removeBtn.addEventListener("click", onRemoveBtnClick)
        }
    }

    return article
}

/**
 * @param {string} postID
 * @returns {Promise<import("../types.js").ToggleLikeOutput>}
 */
function togglePostLike(postID) {
    return doPost(`/api/posts/${encodeURIComponent(postID)}/toggle_like`)
}

/**
 * @param {string} postID
 * @returns {Promise<import("../types.js").ToggleSubscriptionOutput>}
 */
function togglePostSubscription(postID) {
    return doPost(`/api/posts/${encodeURIComponent(postID)}/toggle_subscription`)
}

/**
 * @param {string} timelineItemID
 */
async function removeTimelineItem(timelineItemID) {
    await doDelete("/api/timeline/" + encodeURIComponent(timelineItemID))
}

/**
 * @param {string} postID
 */
async function deletePost(postID) {
    await doDelete("/api/posts/" + encodeURIComponent(postID))
}
