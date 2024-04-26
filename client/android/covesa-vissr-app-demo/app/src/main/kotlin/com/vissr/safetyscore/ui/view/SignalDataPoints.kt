package com.vissr.safetyscore.ui.view

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.material3.Card
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.vissr.safetyscore.ui.VissApiGrpcDemoViewModel
import com.vissr.safetyscore.ui.model.ViewDataPackage

@Composable
fun SignalDataPoints(viewModel: VissApiGrpcDemoViewModel) {
    Column(
        modifier = Modifier
            .fillMaxSize(),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Text(
            modifier = Modifier
                .fillMaxWidth()
                .padding(top = 20.dp),
            textAlign = TextAlign.Center,
            text = "Pay-As-You-Drive VSS datapoints",
            fontFamily = FontFamily.Monospace,
            fontSize = 30.sp
        )
        ConnectionStatus(viewModel = viewModel)
        DataPackages(viewModel = viewModel)
    }
}

@Composable
fun DataPackages(viewModel: VissApiGrpcDemoViewModel) {

    val subscribeResponseMap by viewModel.subscribeResponseMap.collectAsState()

    LazyVerticalGrid(
        columns = GridCells.Adaptive(300.dp),
        modifier = Modifier
            .fillMaxSize()
            .padding(top = 20.dp),
    ) {
        items(subscribeResponseMap.size) { index ->
            val path = subscribeResponseMap.keys.elementAt(index)
            val dataPackage = subscribeResponseMap[path]
            val name = dataPackage?.name ?: ""
            Column {
                Card(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(10.dp)
                ) {
                    DataPackageName(name = name)
                    DataPoints(dataPackage = dataPackage)
                }
                DataPackagePath(path = path)
            }
        }
    }
}

@Composable
private fun DataPackageName(name: String) {
    Text(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 5.dp),
        text = name,
        textAlign = TextAlign.Center,
        fontFamily = FontFamily.Monospace,
        fontSize = 25.sp,
        fontStyle = FontStyle.Normal,
        fontWeight = FontWeight.Bold
    )
}

@Composable
private fun DataPoints(dataPackage: ViewDataPackage?) {
    dataPackage
        ?.dataPoints
        ?.forEach { dataPoint ->
            Text(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(top = 10.dp, bottom = 5.dp),
                text = dataPoint.value,
                textAlign = TextAlign.Center,
                fontFamily = FontFamily.Monospace,
                fontSize = 23.sp,
            )
        }
}

@Composable
private fun DataPackagePath(path: String) {
    Text(
        modifier = Modifier
            .fillMaxWidth()
            .padding(top = 5.dp),
        text = path,
        textAlign = TextAlign.Center,
        fontFamily = FontFamily.Monospace,
    )
}

@Composable
private fun ConnectionStatus(viewModel: VissApiGrpcDemoViewModel) {
    val connectionStatus by viewModel.connectionStatus.collectAsState()

    Row(
        modifier = Modifier
            .fillMaxWidth(),
        horizontalArrangement = Arrangement.Center

    ) {
        Text(
            textAlign = TextAlign.Center,
            text = "VISS Server Connection status: ",
            fontFamily = FontFamily.Monospace,
            fontSize = 30.sp
        )
        Text(
            textAlign = TextAlign.Center,
            text = if (connectionStatus) "Connected" else "Disconnected",
            color = if (connectionStatus) Color.Green else Color.Red,
            fontFamily = FontFamily.Monospace,
            fontSize = 30.sp
        )
    }
}
