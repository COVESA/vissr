package com.vissr.safetyscore.data.datasource.remote.retrofit

import retrofit2.Call
import retrofit2.http.Body
import retrofit2.http.POST

interface AGTService {

    @POST("/agts")
    fun sendAGTRequest(@Body request: AGTRequest): Call<String>
}
