package com.vissr.safetyscore.data.datasource.remote.retrofit

import com.google.gson.annotations.SerializedName

data class ATResponse(
    @SerializedName("action")
    val action: String,
    @SerializedName("aToken")
    val token: String
)
