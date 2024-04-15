package com.vissr.safetyscore.data.datasource.remote

import android.util.Log
import com.google.gson.Gson
import com.vissr.safetyscore.data.datasource.remote.retrofit.AGTClient
import com.vissr.safetyscore.data.datasource.remote.retrofit.AGTRequest
import com.vissr.safetyscore.data.datasource.remote.retrofit.AGTResponse
import com.vissr.safetyscore.data.datasource.remote.retrofit.ATClient
import com.vissr.safetyscore.data.datasource.remote.retrofit.ATRequest
import com.vissr.safetyscore.data.datasource.remote.retrofit.ATResponse
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import retrofit2.Call

interface RetrofitRemoteDataSource {

    fun sendAGTRequest(request: AGTRequest): Flow<String>

    fun sendATRequest(request: ATRequest): Flow<String>

    companion object Factory
}

internal fun RetrofitRemoteDataSource.Factory.build(
    agtClient: AGTClient,
    atClient: ATClient,
): RetrofitRemoteDataSource = RetrofitRemoteDataSourceImpl(
    agtClient = agtClient,
    atClient = atClient,
)

private class RetrofitRemoteDataSourceImpl(
    private val agtClient: AGTClient,
    private val atClient: ATClient,
) : RetrofitRemoteDataSource {

    private val agtResponse = MutableStateFlow("")

    private val atResponse = MutableStateFlow("")

    override fun sendAGTRequest(request: AGTRequest): Flow<String> {

        val call = agtClient.getAGTService().sendAGTRequest(request)

        call.enqueue(object : retrofit2.Callback<String> {

            override fun onResponse(call: Call<String>, response: retrofit2.Response<String>) {
                if (response.isSuccessful) {
                    val serverResponse = response.body()
                    Log.d("RetrofitRemoteDataSource", "sendAGTRequest - onResponse: $serverResponse")

                    val gson = Gson()
                    val agtJsonResponse = gson.fromJson(serverResponse, AGTResponse::class.java)
                    val token = agtJsonResponse.token

                    Log.d("RetrofitRemoteDataSource", "sendAGTRequest - token: $token")

                    agtResponse.value = token
                } else {
                    println("Error: ${response.code()}")
                }
            }

            override fun onFailure(call: Call<String>, t: Throwable) {
                Log.d("RetrofitRemoteDataSource", "Failed to make sendAGTRequest: ${t.message}")
            }
        })

        return agtResponse
    }

    override fun sendATRequest(request: ATRequest): Flow<String> {

        val call = atClient.getATService().sendATRequest(request)

        call.enqueue(object : retrofit2.Callback<String> {

            override fun onResponse(call: Call<String>, response: retrofit2.Response<String>) {
                if (response.isSuccessful) {
                    val serverResponse = response.body()
                    Log.d("RetrofitRemoteDataSource", "sendATRequest - onResponse: $serverResponse")

                    val gson = Gson()
                    val atJsonResponse = gson.fromJson(serverResponse, ATResponse::class.java)
                    val token = atJsonResponse.token

                    Log.d("RetrofitRemoteDataSource", "sendATRequest - token: $token")

                    atResponse.value = token
                } else {
                    Log.d("RetrofitRemoteDataSource", "sendATRequest - Error: ${response.code()}")
                }
            }

            override fun onFailure(call: Call<String>, t: Throwable) {
                Log.d("RetrofitRemoteDataSource", "Failed to make sendATRequest: ${t.message}")
            }
        })

        return atResponse
    }

    private fun Call<String>.enqueue(onSuccess: (String) -> Unit) {

        enqueue(object : retrofit2.Callback<String> {

            override fun onResponse(call: Call<String>, response: retrofit2.Response<String>) {
                if (response.isSuccessful) {
                    val serverResponse = response.body()
                    Log.d("RetrofitRemoteDataSource", "onResponse: $serverResponse")
                    onSuccess(serverResponse ?: "")
                } else {
                    Log.d("RetrofitRemoteDataSource", "Error: ${response.code()}")
                }
            }

            override fun onFailure(call: Call<String>, t: Throwable) {
                Log.d("RetrofitRemoteDataSource", "Failed to make request: ${t.message}")
            }
        })
    }
}
