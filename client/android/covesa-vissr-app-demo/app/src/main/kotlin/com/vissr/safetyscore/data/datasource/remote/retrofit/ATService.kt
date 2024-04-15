package com.vissr.safetyscore.data.datasource.remote.retrofit

import retrofit2.Call
import retrofit2.http.Body
import retrofit2.http.POST

interface ATService {

    @POST("/ats")
    fun sendATRequest(@Body request: ATRequest): Call<String>
}
