/* eslint-disable */
/* tslint:disable */

/**
 * Mock Service Worker.
 * @see https://github.com/mswjs/msw
 * - Please do NOT modify this file.
 */

const PACKAGE_VERSION = '2.14.6'
const INTEGRITY_CHECKSUM = '4db4a41e972cec1b64cc569c66952d82'
const IS_MOCKED_RESPONSE = Symbol('isMockedResponse')
const activeClientIds = new Set()

addEventListener('install', function () {
  self.skipWaiting()
})

addEventListener('activate', function (event) {
  event.waitUntil(self.clients.claim())
})

addEventListener('message', async function (event) {
  const clientId = Reflect.get(event.source || {}, 'id')

  if (!clientId || !self.clients) return
  const client = await self.clients.get(clientId)
  if (!client) return

  const allClients = await self.clients.matchAll({ type: 'window' })

  switch (event.data) {
    case 'KEEPALIVE_REQUEST':
      sendToClient(client, { type: 'KEEPALIVE_RESPONSE' })
      break
    case 'INTEGRITY_CHECK_REQUEST':
      sendToClient(client, {
        type: 'INTEGRITY_CHECK_RESPONSE',
        payload: { packageVersion: PACKAGE_VERSION, checksum: INTEGRITY_CHECKSUM },
      })
      break
    case 'MOCK_ACTIVATE':
      activeClientIds.add(clientId)
      sendToClient(client, {
        type: 'MOCKING_ENABLED',
        payload: { client: { id: client.id, frameType: client.frameType } },
      })
      break
    case 'CLIENT_CLOSED': {
      activeClientIds.delete(clientId)
      const remainingClients = allClients.filter((candidate) => candidate.id !== clientId)
      if (remainingClients.length === 0) self.registration.unregister()
      break
    }
  }
})

addEventListener('fetch', function (event) {
  const requestInterceptedAt = Date.now()
  if (event.request.mode === 'navigate') return
  if (event.request.cache === 'only-if-cached' && event.request.mode !== 'same-origin') return
  if (activeClientIds.size === 0) return

  const requestId = crypto.randomUUID()
  event.respondWith(handleRequest(event, requestId, requestInterceptedAt))
})

/**
 * @param {FetchEvent} event
 * @param {string} requestId
 * @param {number} requestInterceptedAt
 */
async function handleRequest(event, requestId, requestInterceptedAt) {
  const client = await resolveMainClient(event)
  const requestCloneForEvents = event.request.clone()
  const response = await getResponse(event, client, requestId, requestInterceptedAt)

  if (client && activeClientIds.has(client.id)) {
    const serializedRequest = await serializeRequest(requestCloneForEvents)
    const responseClone = response.clone()
    sendToClient(
      client,
      {
        type: 'RESPONSE',
        payload: {
          isMockedResponse: IS_MOCKED_RESPONSE in response,
          request: { id: requestId, ...serializedRequest },
          response: {
            type: responseClone.type,
            status: responseClone.status,
            statusText: responseClone.statusText,
            headers: Object.fromEntries(responseClone.headers.entries()),
            body: responseClone.body,
          },
        },
      },
      responseClone.body ? [serializedRequest.body, responseClone.body] : [],
    )
  }

  return response
}

/** @param {FetchEvent} event */
async function resolveMainClient(event) {
  const client = await self.clients.get(event.clientId)
  if (activeClientIds.has(event.clientId)) return client
  if (client?.frameType === 'top-level') return client

  const allClients = await self.clients.matchAll({ type: 'window' })
  return allClients
    .filter((candidate) => candidate.visibilityState === 'visible')
    .find((candidate) => activeClientIds.has(candidate.id))
}

/**
 * @param {FetchEvent} event
 * @param {Client | undefined} client
 * @param {string} requestId
 * @param {number} requestInterceptedAt
 */
async function getResponse(event, client, requestId, requestInterceptedAt) {
  const requestClone = event.request.clone()

  function passthrough() {
    const headers = new Headers(requestClone.headers)
    const acceptHeader = headers.get('accept')
    if (acceptHeader) {
      const values = acceptHeader.split(',').map((value) => value.trim())
      const filteredValues = values.filter((value) => value !== 'msw/passthrough')
      if (filteredValues.length > 0) headers.set('accept', filteredValues.join(', '))
      else headers.delete('accept')
    }
    return fetch(requestClone, { headers })
  }

  if (!client || !activeClientIds.has(client.id)) return passthrough()

  const serializedRequest = await serializeRequest(event.request)
  const clientMessage = await sendToClient(
    client,
    { type: 'REQUEST', payload: { id: requestId, interceptedAt: requestInterceptedAt, ...serializedRequest } },
    [serializedRequest.body],
  )

  switch (clientMessage.type) {
    case 'MOCK_RESPONSE':
      return respondWithMock(clientMessage.data)
    case 'PASSTHROUGH':
      return passthrough()
  }
  return passthrough()
}

function sendToClient(client, message, transferrables = []) {
  return new Promise((resolve, reject) => {
    const channel = new MessageChannel()
    channel.port1.onmessage = (event) => {
      if (event.data && event.data.error) return reject(event.data.error)
      resolve(event.data)
    }
    client.postMessage(message, [channel.port2, ...transferrables.filter(Boolean)])
  })
}

function respondWithMock(response) {
  if (response.status === 0) return Response.error()
  const mockedResponse = new Response(response.body, response)
  Reflect.defineProperty(mockedResponse, IS_MOCKED_RESPONSE, { value: true, enumerable: true })
  return mockedResponse
}

async function serializeRequest(request) {
  return {
    url: request.url,
    mode: request.mode,
    method: request.method,
    headers: Object.fromEntries(request.headers.entries()),
    cache: request.cache,
    credentials: request.credentials,
    destination: request.destination,
    integrity: request.integrity,
    redirect: request.redirect,
    referrer: request.referrer,
    referrerPolicy: request.referrerPolicy,
    body: await request.arrayBuffer(),
    keepalive: request.keepalive,
  }
}
