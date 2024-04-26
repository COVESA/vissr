package com.vissr.safetyscore.data.datasource.remote.retrofit

import com.google.gson.annotations.SerializedName

data class AGTResponse(
    @SerializedName("action")
    val action: String,
    @SerializedName("token")
    val token: String
)
