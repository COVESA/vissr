package com.vissr.safetyscore.data.datasource.remote.retrofit

private const val AGT_BASE_URL = "http://localhost:7500"

interface AGTClient {
    fun getAGTService(): AGTService

    companion object Factory
}

internal fun AGTClient.Factory.build(): AGTClient = AGTClientImpl()

private class AGTClientImpl : AGTClient {

    // TODO: Add the base URL for the AGTClient
    override fun getAGTService(): AGTService =
        getRetrofit(AGT_BASE_URL).create(AGTService::class.java)
}
