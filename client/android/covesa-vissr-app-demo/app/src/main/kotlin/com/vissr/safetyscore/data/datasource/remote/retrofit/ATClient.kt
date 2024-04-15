package com.vissr.safetyscore.data.datasource.remote.retrofit

private const val AT_BASE_URL = "http://localhost:8600"

interface ATClient {
    fun getATService(): ATService

    companion object Factory
}

internal fun ATClient.Factory.build(): ATClient = ATClientImpl()

private class ATClientImpl : ATClient {

    // TODO: Add the base URL for the ATClient
    override fun getATService(): ATService =
        getRetrofit(AT_BASE_URL).create(ATService::class.java)
}
